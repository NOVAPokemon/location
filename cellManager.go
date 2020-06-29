package main

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/pokemons"
	"github.com/golang/geo/s2"
	log "github.com/sirupsen/logrus"
)

type (
	gymsFromTileValueType = []utils.GymWithServer
	trainerTilesValueType = s2.CellUnion
	nrTrainersInCellValueType = *int32
	PokemonsInCellValueType = utils.WildPokemonWithServer
)

const (
	EarthSurfaceArea = 510064472000000
	TotalSphereArea  = 4 * math.Pi
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

	nrTrainersInCell sync.Map
	createCellLock   sync.Mutex

	trainerCells          sync.Map
	trainersCellsLevel    int
	trainersRegionCoverer s2.RegionCoverer

	PokemonInCell            sync.Map
	pokemonCellsLevel        int
	maxPokemonsPerCell       int
	maxPokemonsPerGeneration int
	pokemonRegionCoverer     s2.RegionCoverer
}

func NewCellManager(gyms []utils.GymWithServer, config *LocationServerConfig) *CellManager {

	if config.GymsCellLevel > config.PokemonCellLevel {
		panic("invalid configs")
	}

	toReturn := &CellManager{
		nrTrainersInCell: sync.Map{},
		trainerCells:     sync.Map{},
		gymsRegionCoverer: s2.RegionCoverer{
			MinLevel: config.GymsCellLevel,
			MaxLevel: config.GymsCellLevel,
			LevelMod: 1,
		},
		pokemonRegionCoverer: s2.RegionCoverer{
			MinLevel: config.PokemonCellLevel,
			MaxLevel: config.PokemonCellLevel,
			LevelMod: 1,
		},
		trainersRegionCoverer: s2.RegionCoverer{
			MinLevel: config.TrainersCellLevel,
			MaxLevel: config.TrainersCellLevel,
			LevelMod: 1,
		},
		maxPokemonsPerGeneration: config.NumberOfPokemonsToGenerate,
		cellsOwned:               config.Cells,
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

	tileNrInterface, ok := cm.trainerCells.Load(trainerId)
	if !ok {
		return errors.New("user was not being tracked")
	}

	tileNrs := tileNrInterface.(trainerTilesValueType)

	for i := range tileNrs {
		trainerNrsValue, ok := cm.nrTrainersInCell.Load(tileNrs[i])
		if !ok {
			panic("user left and was in tile that did not have a counter")
		}

		trainerNrs := trainerNrsValue.(nrTrainersInCellValueType)
		result := atomic.AddInt32(trainerNrs, -1)
		if result == 0 {
			log.Warnf("no trainers in ", tileNrs[i])
			cm.nrTrainersInCell.Delete(tileNrs[i])
		}
	}

	cm.trainerCells.Delete(trainerId)
	return nil
}

func (cm *CellManager) GetTrainerTile(trainerId string) (interface{}, bool) {
	tileNrInterface, ok := cm.trainerCells.Load(trainerId)
	return tileNrInterface, ok
}

func (cm *CellManager) getPokemonsInRegion(region s2.Region) ([]utils.WildPokemonWithServer, error) {

	var pokemonsInTiles []utils.WildPokemonWithServer
	cellNrs := cm.pokemonRegionCoverer.FastCovering(region)

	for cellId := range cellNrs {
		cellInterface, ok := cm.PokemonInCell.Load(cellId)
		if !ok {
			continue
		}
		pokemonsInTiles = append(pokemonsInTiles, cellInterface.(PokemonsInCellValueType))
	}

	return pokemonsInTiles, nil
}

func (cm *CellManager) getGymsInTiles(cellIds s2.CellUnion) []utils.GymWithServer {
	var gymsInTiles []utils.GymWithServer

	for cellId := range cellIds {
		cell := s2.CellFromCellID(cellId)
		if cell.Level() > cm.gymsCellLevel {
			cellId = cell.ID().Parent()
		}
		gymsInTileInterface, ok := cm.gymsInCell.Load(tileNr)

		if !ok {
			return []utils.GymWithServer{}
		}
		/*
			var gymsInVicinity []utils.Gym
			for _, gym := range gymsInTile {
				distance := gps.CalcDistanceBetweenLocations(location, gym.Location)
				if distance <= config.Vicinity {
					gymsInVicinity = append(gymsInVicinity, gym)
				}
			}
		*/
		gymsInTiles = append(gymsInTiles, gymsInTileInterface.(gymsFromTileValueType)...)
	}

	return gymsInTiles
}

// FIXME fix concurrency problem where another thread starts spawning pokemons simultaneously
func (cm *CellManager) generateWildPokemonsForZonePeriodically(cellId s2.CellID) {
	trainersCellId := cellId.Parent(cm.trainersCellsLevel)
	for {
		_, ok := cm.nrTrainersInCell.Load(trainersCellId)
		if !ok {
			log.Warnf("Stopped generating pokemons for zone %d due to cell %d being inactive", cellId, trainersCellId)
			return
		}

		_, ok = cm.PokemonInCell.Load(cellId)
		if ! ok {

			wildPokemon := generateWildPokemon(pokemonSpecies, cellId)
			cm.PokemonInCell.Store(wildPokemon.Pokemon.Id.Hex(), wildPokemon)
			log.Infof("Added wild pokemon to cellId:%d", wildPokemon, cellId)
		} else {
			log.Infof("Skipped cellId:%d", cellId)
		}

		time.Sleep(time.Duration(config.IntervalBetweenGenerations) * time.Second)
	}
}

func (cm *CellManager) RemoveWildPokemonFromCell(cell s2.Cell, pokemonId string) (*pokemons.Pokemon, error) {
	value, ok := cm.PokemonInCell.Load(cell.ID())
	if !ok {
		return nil, errors.New("cell is not loaded")
	}

	pokemon := value.(PokemonsInCellValueType)
	if pokemon.Pokemon.Id.Hex() != pokemonId {
		return nil, errors.New("pokemon not found")
	} else {
		cm.PokemonInCell.Delete(pokemonId)
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
			cm.nrTrainersInCell.Delete(toRemove[i])
			log.Warnf("Disabling tile %d", toRemove[i])
		}
	}

	for i := range toAdd {
		_, ok := cm.nrTrainersInCell.Load(toAdd[i])
		if !ok {
			cm.createCellLock.Lock()
			_, ok = cm.nrTrainersInCell.Load(toAdd[i])
			if ok {
				trainerNrsValue, ok := cm.nrTrainersInCell.Load(toAdd[i])
				if !ok {
					panic("existing tile did not have a trainers counter")
				}

				numTrainers := trainerNrsValue.(nrTrainersInCellValueType)
				atomic.AddInt32(numTrainers, 1)
				cm.createCellLock.Unlock()
				continue
			}
			var numTrainers int32 = 1
			cm.nrTrainersInCell.Store(toAdd[i], &numTrainers)

			cellUnion := s2.CellUnion{toAdd[i]}
			cellUnion.ExpandAtLevel(cm.pokemonCellsLevel)
			for i := range cellUnion {
				go cm.generateWildPokemonsForZonePeriodically(cellUnion[i])
			}
			cm.createCellLock.Unlock()
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

	cm.trainerCells.Store(trainerId, currentTiles)
	return currentTiles, changed, nil
}

func (cm *CellManager) calculateLocationTileChanges(trainerId string, userLoc s2.LatLng) (toRemove, toAdd, currentTiles s2.CellUnion, err error) {
	exitTileCap := cm.CalculateCapForLocation(userLoc, float64(cm.exitBoundarySize))

	// calc cells around user for exit boundary
	newExitCellIds := cm.trainersRegionCoverer.InteriorCellUnion(s2.Region(exitTileCap))

	cm.cellsOwnedLock.RLock()
	if !cm.cellsOwned.Intersects(newExitCellIds) {
		cm.cellsOwnedLock.RUnlock()
		return nil, nil, nil, errors.New("out of bounds of the server")
	}
	cm.cellsOwnedLock.RUnlock()

	entryTileCap := cm.CalculateCapForLocation(userLoc, float64(cm.entryBoundarySize))

	// calc cells around user for entry boundary
	entryCellIds := cm.trainersRegionCoverer.InteriorCellUnion(s2.Region(entryTileCap))

	oldCellIdsInterface, ok := cm.trainerCells.Load(trainerId)
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

	return toRemove, toAdd, currentTiles, nil
}

func (cm *CellManager) CalculateCapForLocation(latLon s2.LatLng, boundarySize float64) s2.Cap {
	s2.PointFromLatLng(s2.LatLng{})
	return s2.CapFromCenterArea(s2.PointFromLatLng(latLon), boundarySize*TotalSphereArea/EarthSurfaceArea)
}

func (cm *CellManager) LoadGyms(gyms []utils.GymWithServer) {
	for _, gymWithSrv := range gyms {
		gym := gymWithSrv.Gym
		cellId := s2.CellIDFromLatLng(gym.Location)
		if cm.cellsOwned.ContainsCellID(cellId) {
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
	cm.trainerCells.Range(func(_, _ interface{}) bool {
		numUsers++
		return true
	})
	log.Infof("Number of active users: %d", numUsers)
	// log.Info(cm.trainerCells)
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
		tile.pokemons.Range(func(key, value interface{}) bool {
			nrPokemons++
			return true
		})
		log.Info("Number of generated pokemons: ", nrPokemons)
		return true
	})
	log.Infof("Number of active tiles: %d", counter)
}
*/

func (cm *CellManager) AddGym(gymWithSrv utils.GymWithServer) error {
	cellId := s2.CellIDFromLatLng(gymWithSrv.Gym.Location)

	if !cm.cellsOwned.ContainsCellID(cellId) {
		return errors.New("out of bounds of server")
	}

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
