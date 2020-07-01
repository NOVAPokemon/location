package main

import (
	"errors"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/pokemons"
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
	EarthRadiusInMeter = 6378000
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

func NewCellManager(gyms []utils.GymWithServer, config *LocationServerConfig) *CellManager {

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
		cellsOwned:      s2.CellUnion{},
		totalNrTrainers: new(int64),
	}

	toReturn.LoadGyms(gyms)
	go toReturn.logActiveGymsPeriodic()
	return toReturn
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

func (cm *CellManager) RemoveTrainerLocation(trainerId string) error {

	tileNrsInterface, ok := cm.lastTrainerCells.Load(trainerId)
	if !ok {
		return errors.New("user was not being tracked")
	}
	tileNrs := tileNrsInterface.(trainerTilesValueType)

	for i := range tileNrs {
		if activeCellValue, ok := cm.activeCells.Load(tileNrs[i]); ok {
			activeCell := activeCellValue.(activeCellsValueType)
			nrTrainersInTile := activeCell.RemoveTrainer()
			if nrTrainersInTile == 0 {
				cm.changeTrainerCellsLock.Lock() // ensures no one else is creating or deleting the tile
				if _, ok := cm.activeCells.Load(tileNrs[i]); ok {
					activeCell.AcquireWriteLock()
					cm.activeCells.Delete(tileNrs[i])
					activeCell.ReleaseWriteLock()
				}
				cm.changeTrainerCellsLock.Unlock()
			}
		}
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

func (cm *CellManager) generateWildPokemonsForServerPeriodically() {
	log.Infof("starting pokemon generation")

	for {
		// TODO change this for new struct
		trainerCellsToObject := cm.activeCells

		trainerCellsToObject.Range(func(trainerCellIdInterface, activeCellInterface interface{}) bool {
			trainerCellId := trainerCellIdInterface.(s2.CellID)
			trainerCell := s2.CellFromCellID(trainerCellId)

			activeCell := activeCellInterface.(ActiveCell)

			toGenerate := int(activeCell.GetNrTrainers()) * config.PokemonsToGeneratePerTrainerCell
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
			activeCell.AddPokemons(pokemonGenerated)
			return true
		})

		sleepDuration := time.Duration(float64(config.IntervalBetweenGenerations)) * time.Second
		time.Sleep(sleepDuration)
	}
}

func (cm *CellManager) RemoveWildPokemonFromCell(cell s2.Cell, pokemonId string) (*pokemons.Pokemon, error) {
	value, ok := cm.PokemonInCell.Load(cell.ID())
	if !ok {
		return nil, errors.New("cell has no pokemon")
	}

	pokemon := value.(PokemonsInCellValueType)
	if pokemon.Pokemon.Id.Hex() != pokemonId {
		return nil, errors.New("pokemon not found")
	} else {
		cm.PokemonInCell.Delete(cell.ID())
		return &pokemon.Pokemon, nil
	}
}

// auxiliary functions

func (cm *CellManager) UpdateTrainerTiles(trainerId string, loc s2.LatLng) (s2.CellUnion, bool, error) {
	toRemove, toAdd, currentTiles, err := cm.calculateLocationTileChanges(trainerId, loc)
	if err != nil {
		return nil, false, err
	}

	changed := len(toRemove) > 0 || len(toAdd) > 0

	for i := range toRemove {
		trainerNrsValue, ok := cm.nrTrainersInCell.Load(toRemove[i])
		if !ok {
			log.Warn("server was removing tile that did not have a counter")
			continue
		}

		numTrainers := trainerNrsValue.(nrTrainersInCellValueType)
		result := atomic.AddInt32(numTrainers, -1)
		if result == 0 {
			cm.changeCellsLock.Lock()
			cm.nrTrainersInCell.Delete(toRemove[i])
			cm.changeCellsLock.Unlock()
			log.Warnf("Disabling tile %d", toRemove[i])
		}
	}

	for i := range toAdd {
		_, ok := cm.nrTrainersInCell.Load(toAdd[i])
		if !ok {
			cm.changeCellsLock.Lock()
			_, ok = cm.nrTrainersInCell.Load(toAdd[i])
			if ok {
				trainerNrsValue, ok := cm.nrTrainersInCell.Load(toAdd[i])
				if !ok {
					panic("existing tile did not have a trainers counter")
				}

				numTrainers := trainerNrsValue.(nrTrainersInCellValueType)
				atomic.AddInt32(numTrainers, 1)
				cm.changeCellsLock.Unlock()
				continue
			}
			var numTrainers int32 = 1
			cm.nrTrainersInCell.Store(toAdd[i], &numTrainers)
			cellUnion := s2.CellUnion{toAdd[i]}
			expandUnionToLevel(cellUnion, cm.pokemonCellsLevel)

			// TODO this can go to after store no?
			cm.changeCellsLock.Unlock()
			continue
		} else {

			trainerNrsValue, ok := cm.nrTrainersInCell.Load(toAdd[i])
			if !ok {
				log.Warn("existing tile did not have a trainers counter")
				continue
			}
			numTrainers := trainerNrsValue.(nrTrainersInCellValueType)
			atomic.AddInt32(numTrainers, 1)
		}
	}
	cm.lastTrainerCells.Store(trainerId, currentTiles)
	return currentTiles, changed, nil
}

func (cm *CellManager) calculateLocationTileChanges(trainerId string, userLoc s2.LatLng) (toRemove, toAdd,
	currentTiles s2.CellUnion, err error) {
	exitTileCap := CalculateCapForLocation(userLoc, float64(cm.exitBoundarySize))

	// calc cells around user for exit boundary
	newExitCellIds := cm.trainersRegionCoverer.InteriorCellUnion(s2.Region(exitTileCap))

	cm.cellsOwnedLock.RLock()
	if !cm.cellsOwned.Intersects(newExitCellIds) {
		cm.cellsOwnedLock.RUnlock()
		return nil, nil, nil,
			errors.New("server cells do not intersect client exit boundaries")
	}
	cm.cellsOwnedLock.RUnlock()

	entryTileCap := CalculateCapForLocation(userLoc, float64(cm.entryBoundarySize))

	// calc cells around user for entry boundary
	entryCellIds := cm.trainersRegionCoverer.InteriorCellUnion(s2.Region(entryTileCap))

	oldCellIdsInterface, ok := cm.lastTrainerCells.Load(trainerId)
	if !ok {
		return nil, entryCellIds, entryCellIds, errors.New("")
	}

	oldCellIds := oldCellIdsInterface.(trainerTilesValueType)

	// calc diff from  old cells to new exit boundary to find which cells to remove
	toRemove = s2.CellUnionFromDifference(oldCellIds, newExitCellIds)

	// calc diff from new entry cells to old cells to find which cells to add
	toAdd = s2.CellUnionFromDifference(entryCellIds, oldCellIds)

	// calculates which cells user was on
	cellsToKeep := s2.CellUnionFromIntersection(newExitCellIds, oldCellIds)

	// adds tiles to keep and new tiles to load in orded to return which cells user should load
	currentTiles = s2.CellUnionFromUnion(toAdd, cellsToKeep)

	// TODO normalize cells to trainers level?

	return toRemove, toAdd, currentTiles, nil
}

func CalculateCapForLocation(latLon s2.LatLng, boundarySize float64) s2.Cap {
	angle := s1.Angle(boundarySize / EarthRadiusInMeter)
	return s2.CapFromCenterAngle(s2.PointFromLatLng(latLon), angle)
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
