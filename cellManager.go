package main

import (
	"errors"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/NOVAPokemon/utils"
	"github.com/golang/geo/s1"
	"github.com/golang/geo/s2"
	log "github.com/sirupsen/logrus"
)

type (
	gymsFromTileValueType = []utils.GymWithServer
	trainerTilesValueType = s2.CellUnion
	activeCellsValueType = ActiveCell
)

const (
	EarthRadiusInMeter = 6371000
	maxCells           = 1000
)

type CellManager struct {
	pokemonSpecies []string

	entryBoundarySize int // meters
	exitBoundarySize  int // meters

	cellsOwned     s2.CellUnion
	cellsOwnedLock sync.RWMutex

	gymsInCell        sync.Map
	gymsCellLevel     int
	gymsRegionCoverer s2.RegionCoverer

	totalNrTrainers *int64

	changeTrainerCellsLock sync.Mutex
	activeCells            sync.Map

	lastTrainerCells      sync.Map
	trainersCellsLevel    int
	trainersRegionCoverer s2.RegionCoverer

	pokemonCellsLevel    int
	pokemonRegionCoverer s2.RegionCoverer
}

func NewCellManager(gyms []utils.GymWithServer, config *LocationServerConfig, cellsOwned s2.CellUnion) *CellManager {

	if config.GymsCellLevel > config.PokemonCellLevel {
		panic("invalid configs")
	}

	toReturn := &CellManager{
		activeCells:            sync.Map{},
		lastTrainerCells:       sync.Map{},
		changeTrainerCellsLock: sync.Mutex{},
		gymsRegionCoverer: s2.RegionCoverer{
			MinLevel: config.GymsCellLevel,
			MaxLevel: config.GymsCellLevel,
			LevelMod: 1,
			MaxCells: maxCells,
		},
		pokemonRegionCoverer: s2.RegionCoverer{
			MinLevel: config.PokemonCellLevel,
			MaxLevel: config.PokemonCellLevel,
			LevelMod: 1,
			MaxCells: maxCells,
		},
		trainersRegionCoverer: s2.RegionCoverer{
			MinLevel: config.TrainersCellLevel,
			MaxLevel: config.TrainersCellLevel,
			LevelMod: 1,
			MaxCells: maxCells,
		},
		cellsOwned:      cellsOwned,
		totalNrTrainers: new(int64),
	}

	toReturn.LoadGyms(gyms)
	go toReturn.logActiveGymsPeriodic()
	return toReturn
}

func (cm *CellManager) RemoveTrainerLocation(trainerId string) error {
	tileNrsInterface, ok := cm.lastTrainerCells.Load(trainerId)
	if !ok {
		return errors.New("user was not being tracked")
	}
	tileNrs := tileNrsInterface.(trainerTilesValueType)
	for _, cellID := range tileNrs {
		cm.removeTrainerFromCell(cellID)
	}
	cm.lastTrainerCells.Delete(trainerId)
	return nil
}

func (cm *CellManager) GetTrainerTile(trainerId string) (interface{}, bool) {
	tileNrInterface, ok := cm.lastTrainerCells.Load(trainerId)
	return tileNrInterface, ok
}

func (cm *CellManager) getPokemonsInCells(cellIds s2.CellUnion) []utils.WildPokemonWithServer {
	var pokemonsInTiles []utils.WildPokemonWithServer
	cellIdsNormalized := expandUnionToLevel(cellIds, cm.trainersCellsLevel)

	for cellId := range cellIdsNormalized {
		cellInterface, ok := cm.activeCells.Load(cellId)
		if !ok {
			continue
		}
		cell := cellInterface.(activeCellsValueType)
		pokemonsInTiles = append(pokemonsInTiles, cell.GetPokemonsInCell()...)
	}

	return pokemonsInTiles
}

func (cm *CellManager) getGymsInCells(cellIds s2.CellUnion) []utils.GymWithServer {
	var (
		gymsInCells []utils.GymWithServer
	)

	cellIdsNormalized := expandUnionToLevel(cellIds, cm.gymsCellLevel)

	for _, cellId := range cellIdsNormalized {
		gymsInTileInterface, ok := cm.gymsInCell.Load(cellId)
		if !ok {
			continue
		}
		gymsInCells = append(gymsInCells, gymsInTileInterface.(gymsFromTileValueType)...)
	}

	return gymsInCells
}

func (cm *CellManager) generateWildPokemonsForServerPeriodically() {
	log.Infof("starting pokemon generation")

	for {
		// TODO change this for new struct
		cm.activeCells.Range(func(trainerCellIdInterface, activeCellInterface interface{}) bool {
			trainerCellId := trainerCellIdInterface.(s2.CellID)
			trainerCell := s2.CellFromCellID(trainerCellId)
			activeCell := activeCellInterface.(activeCellsValueType)
			toGenerate := int(activeCell.GetNrTrainers()) * config.PokemonsToGeneratePerTrainerCell
			log.Info("Generating %d pokemons for cell %d", toGenerate, activeCell.cellID)
			pokemonGenerated := make([]utils.WildPokemonWithServer, toGenerate)
			var randomCellId s2.CellID
			for numGenerated := 0; numGenerated < toGenerate; {
				cellRect := trainerCell.RectBound()
				centerRect := cellRect.Center()

				size := cellRect.Size()
				deltaLat := rand.Float64()*size.Lat.Degrees()*2 - size.Lat.Degrees()
				deltaLng := rand.Float64()*size.Lng.Degrees()*2 - size.Lng.Degrees()

				randomLatLng := s2.LatLngFromDegrees(centerRect.Lat.Degrees()+deltaLat,
					centerRect.Lng.Degrees()+deltaLng)

				randomCell := s2.CellFromCellID(randomCellId)
				randomCellId = s2.CellFromLatLng(randomLatLng).ID().Parent(cm.pokemonCellsLevel)

				if trainerCell.ContainsCell(randomCell) {
					numGenerated++
					wildPokemon := generateWildPokemon(pokemonSpecies, randomCellId)
					log.Infof("Added wild pokemon %s to cellId: %d", wildPokemon.Pokemon.Id.Hex(), randomCellId)
					pokemonGenerated = append(pokemonGenerated, wildPokemon)
				} else {
					log.Info("randomized point for pokemon generation ended up outside of boundaries")
				}
			}
			activeCell.AcquireReadLock()
			if activeCellInterface, ok := cm.activeCells.Load(activeCell.cellID); ok {
				activeCell := activeCellInterface.(activeCellsValueType)
				activeCell.AddPokemons(pokemonGenerated)
			}
			activeCell.ReleaseReadLock()
			return true
		})
		sleepDuration := time.Duration(float64(config.IntervalBetweenGenerations)) * time.Second
		time.Sleep(sleepDuration)
	}
}

func (cm *CellManager) RemoveWildPokemonFromCell(activeCellID s2.Cell, toDelete utils.WildPokemonWithServer) (*utils.WildPokemonWithServer, error) {
	activeCellInterface, ok := cm.activeCells.Load(activeCellID)
	if !ok {
		return nil, errors.New("cell non existing")
	}
	activeCell := activeCellInterface.(ActiveCell)

	if activeCell.RemovePokemon(toDelete) {
		return &toDelete, nil
	} else {
		return nil, newPokemonNotFoundError(toDelete.Pokemon.Id.Hex())
	}
}

func (cm *CellManager) GetPokemon(activeCellID s2.Cell, pokemon utils.WildPokemonWithServer) (*utils.WildPokemonWithServer, error) {
	activeCellInterface, ok := cm.activeCells.Load(activeCellID)
	if !ok {
		return nil, errors.New("cell non existing")
	}
	activeCell := activeCellInterface.(ActiveCell)

	if pokemon, ok := activeCell.GetPokemon(pokemon); ok {
		return pokemon, nil
	} else {
		return nil, newPokemonNotFoundError(pokemon.Pokemon.Id.Hex())
	}
}

// auxiliary functions

func (cm *CellManager) UpdateTrainerTiles(trainerId string, loc s2.LatLng) (s2.CellUnion, bool, error) {
	cellsToRemove, cellsToAdd, currentCells, err := cm.calculateLocationTileChanges(trainerId, loc)
	if err != nil {
		return nil, false, err
	}
	changed := len(cellsToRemove) > 0 || len(cellsToAdd) > 0
	for _, cellID := range cellsToRemove {
		log.Infof("User %s left cell %d", trainerId, cellID)
		cm.removeTrainerFromCell(cellID)
	}

	for _, cellID := range cellsToAdd {
		log.Infof("User %s joined cell %d", trainerId, cellID)
		cm.addTrainerToCell(cellID)
	}
	cm.lastTrainerCells.Store(trainerId, currentCells)
	return currentCells, changed, nil
}

func (cm *CellManager) calculateLocationTileChanges(trainerId string, userLoc s2.LatLng) (toRemove, toAdd,
	currentTiles s2.CellUnion, err error) {
	exitTileCap := CalculateCapForLocation(userLoc, float64(cm.exitBoundarySize))

	// calc cells around user for exit boundary
	newExitCellIds := cm.trainersRegionCoverer.Covering(s2.Region(exitTileCap))
	log.Infof("User exit region covers %d cells", len(newExitCellIds))
	cm.cellsOwnedLock.RLock()
	if !newExitCellIds.Intersects(cm.cellsOwned) {
		cm.cellsOwnedLock.RUnlock()
		return nil, nil, nil,
			errors.New("server cells do not intersect client exit boundaries")
	}
	cm.cellsOwnedLock.RUnlock()
	entryTileCap := CalculateCapForLocation(userLoc, float64(cm.entryBoundarySize))

	// calc cells around user for entry boundary
	entryCellIds := cm.trainersRegionCoverer.Covering(s2.Region(entryTileCap))
	log.Infof("User entry region covers %d cells", len(entryCellIds))

	oldCellIdsInterface, ok := cm.lastTrainerCells.Load(trainerId)
	var oldCellIds trainerTilesValueType
	if ok {
		oldCellIds = oldCellIdsInterface.(trainerTilesValueType)
	} else {
		oldCellIds = s2.CellUnion{}
	}

	// calc diff from  old cells to new exit boundary to find which cells to remove
	toRemove = s2.CellUnionFromDifference(oldCellIds, newExitCellIds)

	// calc which cells to add by obtaining the difference between the new cells against the old ones
	toAdd = s2.CellUnionFromDifference(entryCellIds, oldCellIds)

	// discard cells which are not in the new exit boundary
	cellsToKeep := s2.CellUnionFromIntersection(newExitCellIds, oldCellIds)

	// adds tiles to keep and new tiles to load in orded to return which cells user should load
	currentTiles = s2.CellUnionFromUnion(toAdd, cellsToKeep)

	return toRemove, toAdd, currentTiles, nil
}

func CalculateCapForLocation(latLon s2.LatLng, boundarySize float64) s2.Cap {
	point := s2.PointFromLatLng(latLon)
	angle := s1.Angle(boundarySize / EarthRadiusInMeter)
	return s2.CapFromCenterAngle(point, angle)
}

func (cm *CellManager) LoadGyms(gyms []utils.GymWithServer) {
	for _, gymWithSrv := range gyms {
		gym := gymWithSrv.Gym
		cellId := s2.CellIDFromLatLng(gym.Location)

		cm.cellsOwnedLock.RLock()
		if cm.cellsOwned.ContainsCellID(cellId) {
			cm.cellsOwnedLock.RUnlock()
			parent := cellId.Parent(cm.gymsCellLevel)
			gymsInterface, ok := cm.gymsInCell.Load(parent)
			var gyms gymsFromTileValueType
			if !ok {
				gyms = gymsFromTileValueType{}
			} else {
				gyms = gymsInterface.(gymsFromTileValueType)
			}
			gyms = append(gyms, gymWithSrv)
			cm.gymsInCell.Store(parent, gyms)
		} else {
			cm.cellsOwnedLock.RUnlock()
			log.Infof("Gym %s out of bounds", gym.Name)
		}
	}
	cm.gymsInCell.Range(func(tileNr, gyms interface{}) bool {
		log.Infof("Tile %d gyms: %+v", tileNr, gyms)
		return true
	})
}

/*
func (cm *CellManager) logTileManagerState() {
	numUsers := 0
	cm.lastTrainerCells.Range(func(_, _ interface{}) bool {
		numUsers++
		return true
	})
	log.Infof("Number of active users: %d", numUsers)
	// log.Info(cm.lastTrainerCells)
	counter := 0

	cm..Range(func(tileNr, tileInterface interface{}) bool {
		tile := tileInterface.(activeTileValueType)
		counter++
		log.Infof("---------------------Tile %d---------------------", tileNr)
		log.Infof("Tile bounds TopLeft:%+v, TopRight:%+v", tile.TopLeftCorner, tile.BotRightCorner)

		numTrainersValue, ok := cm.nrTrainersInCell.Load(tileNr)
		if !ok {
			log.Warn("tried to get number of trainers on active tile and counter was missing")
			return true
		}

		numTrainers := numTrainersValue.(activeTileTrainerNrValueType)
		log.Info("Number of active users: ", numTrainers)
		nrPokemons := 0
		tile.wildPokemons.Range(func(key, value interface{}) bool {
			nrPokemons++
			return true
		})
		log.Info("Number of generated wildPokemons: ", nrPokemons)
		return true
	})
	log.Infof("Number of active tiles: %d", counter)
}
*/

func (cm *CellManager) AddGym(gymWithSrv utils.GymWithServer) error {
	cellId := s2.CellIDFromLatLng(gymWithSrv.Gym.Location)

	cm.cellsOwnedLock.RLock()
	if !cm.cellsOwned.ContainsCellID(cellId) {
		cm.cellsOwnedLock.RUnlock()
		return errors.New("out of bounds of server")
	}
	cm.cellsOwnedLock.RUnlock()

	cellIdAtGymsLevel := cellId.Parent(cm.gymsCellLevel)
	gymsInterface, ok := cm.gymsInCell.Load(cellIdAtGymsLevel)
	var gyms gymsFromTileValueType
	if ok {
		gyms = gymsInterface.(gymsFromTileValueType)
		for i := 0; i < len(gyms); i++ {
			if gyms[i].Gym.Name == gymWithSrv.Gym.Name {
				return nil
			}
		}
	} else {
		gyms = gymsFromTileValueType{}
	}
	cm.gymsInCell.Store(cellIdAtGymsLevel, append(gyms, gymWithSrv))
	return nil
}

func (cm *CellManager) SetServerCells(newCells s2.CellUnion) {
	log.Infof("Loaded boundaries:")
	for _, v := range newCells {
		log.Infof("Loaded cell: %d", v)
	}

	cm.cellsOwnedLock.Lock()
	cm.cellsOwned = newCells
	cm.cellsOwnedLock.Unlock()
}

func (cm *CellManager) SetGyms(gymWithSrv []utils.GymWithServer) error {
	for _, gymWithSrv := range gymWithSrv {
		if err := cm.AddGym(gymWithSrv); err != nil {
			log.Error(WrapSetGymsError(err, gymWithSrv.Gym.Name))
		}
	}
	return nil
}

func (cm *CellManager) logActiveGymsPeriodic() {
	for {
		log.Info("Active gyms:")
		cm.gymsInCell.Range(func(k, v interface{}) bool {
			for _, gym := range v.(gymsFromTileValueType) {
				log.Infof("Gym name: %s, Gym location: %+v", gym.Gym.Name, gym.Gym.Location)
			}
			return true
		})
		time.Sleep(30 * time.Second)
	}
}

func (cm *CellManager) removeTrainerFromCell(cellID s2.CellID) {
	if activeCellValue, ok := cm.activeCells.Load(cellID); ok {
		activeCell := activeCellValue.(activeCellsValueType)
		var nrTrainersInTile int64
		activeCell = activeCellValue.(activeCellsValueType)
		nrTrainersInTile = activeCell.RemoveTrainer()
		if nrTrainersInTile == 0 {
			cm.changeTrainerCellsLock.Lock() // ensures no one else is creating or deleting the tile
			if cellValue, ok := cm.activeCells.Load(cellID); ok {
				cell := cellValue.(activeCellsValueType)
				cell.AcquireWriteLock()
				if cell.GetNrTrainers() == 0 { // assure no other user incremented in the meantime
					cm.activeCells.Delete(cell.cellID)
				}
				cell.ReleaseWriteLock()
			}
			cm.changeTrainerCellsLock.Unlock()
		}
	}
}

func (cm *CellManager) addTrainerToCell(cellID s2.CellID) {
	activeCellValue, ok := cm.activeCells.Load(cellID)
	if ok {
		activeCell := activeCellValue.(activeCellsValueType)
		activeCell.AcquireReadLock()
		if activeCellValue, ok := cm.activeCells.Load(cellID); ok {
			activeCell := activeCellValue.(activeCellsValueType)
			activeCell.AddTrainer()
		} else { // cell was deleted in the meantime, try to add again
			activeCell.ReleaseReadLock()
			cm.addTrainerToCell(cellID)
			return
		}
		activeCell.ReleaseReadLock()
	} else {
		cm.changeTrainerCellsLock.Lock()
		if activeCellValue, ok := cm.activeCells.Load(cellID); ok {
			// cell was added in the meantime, and no other thread can remove/add in the meantime, so no need to acquire read lock
			activeCell := activeCellValue.(activeCellsValueType)
			activeCell.AddTrainer()
		} else {
			newCell := NewActiveCell(cellID, cm.pokemonCellsLevel)
			newCell.AddTrainer()
			cm.activeCells.Store(cellID, *newCell)
		}
		cm.changeTrainerCellsLock.Unlock()
	}
}

func convertStringsToCellIds(cellIdsStrings []string) s2.CellUnion {
	cells := make(s2.CellUnion, len(cellIdsStrings))
	for i, cellId := range cellIdsStrings {
		if id, err := strconv.ParseUint(cellId, 10, 64); err == nil {
			cells[i] = s2.CellID(id)
		} else {
			panic("error loading config")
		}
	}
	return cells
}

func expandUnionToLevel(cellIds s2.CellUnion, level int) s2.CellUnion {
	cellIdsNormalized := s2.CellUnion{}

	for _, cellId := range cellIds {
		cell := s2.CellFromCellID(cellId)
		if cell.Level() > level {
			cellId = cell.ID().Parent(level)
			cellIdsNormalized = append(cellIdsNormalized, cellId)
		} else if cell.Level() < level {
			childrenAtLevel := s2.CellUnion{cellId}
			childrenAtLevel.Denormalize(cm.gymsCellLevel, 1)
			cellIdsNormalized = append(cellIdsNormalized, childrenAtLevel...)
		}
	}

	if !cellIdsNormalized.IsValid() {
		panic("cells are not valid")
	}

	return cellIdsNormalized
}
