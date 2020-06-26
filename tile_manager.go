package main

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NOVAPokemon/utils"
	locationUtils "github.com/NOVAPokemon/utils/location"
	"github.com/NOVAPokemon/utils/pokemons"
	"github.com/golang/geo/r2"
	"github.com/golang/geo/s2"
	log "github.com/sirupsen/logrus"
)

type (
	gymsFromTileValueType        = []utils.GymWithServer
	activeTileValueType          = *Tile
	trainerTilesValueType        = []int
	activeTileTrainerNrValueType = *int32
)

type TileManager struct {
	pokemonSpecies           []string
	numTilesInWorld          int
	boundariesLock           sync.RWMutex
	gymsFromTile             sync.Map
	activeTiles              sync.Map
	trainerTiles             sync.Map
	numTilesPerAxis          int
	tileSideLength           float64
	maxPokemonsPerTile       int
	maxPokemonsPerGeneration int
	entryBoundarySize        int
	exitBoundarySize         int
	activateTileLock         sync.Mutex
	serverRect               r2.Rect
	activeTileTrainerNumber  sync.Map
}

type Tile struct {
	pokemons       sync.Map
	TopLeftCorner  utils.Location
	BotRightCorner utils.Location
}

const LatitudeMax = 85.05115

func NewTileManager(gyms []utils.GymWithServer, numTiles, maxPokemonsPerTile, pokemonsPerGeneration int,
	topLeft utils.Location, botRight utils.Location) *TileManager {

	numTilesPerAxis := int(math.Sqrt(float64(numTiles)))
	tileSide := 360.0 / float64(numTilesPerAxis)
	serverRect := locationUtils.LocationsToRect(topLeft, botRight)

	toReturn := &TileManager{
		numTilesInWorld:          numTiles,
		activeTiles:              sync.Map{},
		trainerTiles:             sync.Map{},
		numTilesPerAxis:          numTilesPerAxis,
		tileSideLength:           tileSide,
		maxPokemonsPerTile:       maxPokemonsPerTile,
		maxPokemonsPerGeneration: pokemonsPerGeneration,
		activateTileLock:         sync.Mutex{},
		serverRect:               serverRect,
		activeTileTrainerNumber:  sync.Map{},
	}

	toReturn.LoadGyms(gyms)
	go toReturn.logActiveGymsPeriodic()
	return toReturn
}

func (tm *TileManager) logActiveGymsPeriodic() {
	for {
		log.Info("Active gyms:")
		tm.gymsFromTile.Range(func(k, v interface{}) bool {
			for _, gym := range v.(gymsFromTileValueType) {
				log.Infof("Gym name: %s, Gym location: %+v", gym.Gym.Name, gym.Gym.Location)
			}
			return true
		})
		time.Sleep(30 * time.Second)
	}
}

func (tm *TileManager) RemoveTrainerLocation(trainerId string) error {
	tileNrInterface, ok := tm.trainerTiles.Load(trainerId)
	if !ok {
		return errors.New("user was not being tracked")
	}

	tileNrs := tileNrInterface.(trainerTilesValueType)

	for i := range tileNrs {
		trainerNrsValue, ok := tm.activeTileTrainerNumber.Load(tileNrs[i])
		if !ok {
			panic("user left and was in tile that did not have a counter")
		}

		trainerNrs := trainerNrsValue.(activeTileTrainerNrValueType)
		result := atomic.AddInt32(trainerNrs, -1)
		if result == 0 {
			log.Warnf("Disabling tile %d", tileNrs[i])
			tm.activeTiles.Delete(tileNrs[i])
			tm.activeTileTrainerNumber.Delete(tileNrs[i])
		}
	}

	tm.trainerTiles.Delete(trainerId)
	return nil
}

func (tm *TileManager) GetTrainerTile(trainerId string) (interface{}, bool) {
	tileNrInterface, ok := tm.trainerTiles.Load(trainerId)
	return tileNrInterface, ok
}

func (tm *TileManager) getPokemonsInTiles(tileNrs ...int) ([]utils.WildPokemonWithServer, error) {
	var pokemonsInTiles []utils.WildPokemonWithServer

	for tileNr := range tileNrs {
		tileInterface, ok := tm.activeTiles.Load(tileNr)
		if !ok {
			continue
		}

		tile := tileInterface.(activeTileValueType)
		tile.pokemons.Range(func(key, value interface{}) bool {
			pokemonsInTiles = append(pokemonsInTiles, value.(utils.WildPokemonWithServer))
			return true
		})
	}

	return pokemonsInTiles, nil
}

func (tm *TileManager) getGymsInTiles(tileNrs ...int) []utils.GymWithServer {
	var gymsInTiles []utils.GymWithServer

	for tileNr := range tileNrs {
		gymsInTileInterface, ok := tm.gymsFromTile.Load(tileNr)

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

func (tm *TileManager) generateWildPokemonsForZonePeriodically(zoneNr int) {
	for {
		tileInterface, ok := tm.activeTiles.Load(zoneNr)
		if !ok {
			log.Warnf("Stopped generating pokemons for zone %d due to missing zone", zoneNr)
			return
		}
		tile := tileInterface.(*Tile)

		log.Info("Refreshing wild pokemons...")
		nrPokemons := 0
		tile.pokemons.Range(func(key, value interface{}) bool {
			nrPokemons++
			return true
		})

		nrToGenerate := tm.maxPokemonsPerTile - nrPokemons
		if nrToGenerate > tm.maxPokemonsPerGeneration {
			nrToGenerate = tm.maxPokemonsPerGeneration
		}
		if nrToGenerate < 0 {
			nrToGenerate = 0
		}
		wildPokemons := generateWildPokemons(nrToGenerate, pokemonSpecies, tile.TopLeftCorner, tile.BotRightCorner)
		for _, wildPokemon := range wildPokemons {
			tile.pokemons.Store(wildPokemon.Pokemon.Id.Hex(), wildPokemon)
		}
		log.Infof("Added %d pokemons to zone %d", len(wildPokemons), zoneNr)
		time.Sleep(time.Duration(config.IntervalBetweenGenerations) * time.Second)
	}
}

func (tm *TileManager) cleanWildPokemons(tileNr int) {
	tileInterface, ok := tm.activeTiles.Load(tileNr)
	if ok {
		tile := tileInterface.(activeTileValueType)
		tile.pokemons = sync.Map{}
	}
}

func (tm *TileManager) RemoveWildPokemonFromTile(tileNr int, pokemonId string) (*pokemons.Pokemon, error) {
	tileInterface, ok := tm.activeTiles.Load(tileNr)
	if !ok {
		return nil, errors.New("tile is nil")
	}

	tile := tileInterface.(activeTileValueType)
	value, ok := tile.pokemons.Load(pokemonId)
	if !ok {
		return nil, errors.New("pokemon not found")
	} else {
		tile.pokemons.Delete(pokemonId)
		toReturn := value.(utils.WildPokemonWithServer).Pokemon
		return &toReturn, nil
	}
}

// auxiliary functions

func (tm *TileManager) GetTileNrFromLocation(location utils.Location) (int, int, int, error) {
	if location.Latitude > LatitudeMax || location.Latitude < -LatitudeMax {
		return -1, -1, -1, errors.New("latitude value out of bounds, bound is: -[85.05115 : 85.05115]")
	}
	if location.Longitude > 179.9 || location.Longitude < -179.9 {
		return -1, -1, -1, errors.New("latitude value out of bounds, bound is: [-179.9 : 179.9]")
	}

	latLong := s2.LatLngFromDegrees(location.Latitude, location.Longitude)

	proj := s2.NewMercatorProjection(180)
	transformedPoint := proj.FromLatLng(latLong)

	// translates top left corner to 0,0 and flips to maintain order
	tileCol := int(math.Floor((180 + transformedPoint.X) / tm.tileSideLength))
	tileRow := int(math.Floor((180 - transformedPoint.Y) / tm.tileSideLength))
	tileNr := tileRow*tm.numTilesPerAxis + tileCol
	return tileNr, tileRow, tileCol, nil
}

func (tm *TileManager) UpdateTrainerTiles(trainerId string, row, column int) ([]int, bool) {
	toRemove, toAdd, currentTiles := tm.calculateLocationTileChanges(trainerId, row, column)

	changed := len(toRemove) > 0 || len(toAdd) > 0

	for i := range toRemove {
		_, ok := tm.activeTiles.Load(toRemove[i])
		if !ok {
			continue
		}

		trainerNrsValue, ok := tm.activeTileTrainerNumber.Load(toRemove[i])
		if !ok {
			log.Warn("server was removing tile that did not have a counter")
			continue
		}

		numTrainers := trainerNrsValue.(activeTileTrainerNrValueType)
		result := atomic.AddInt32(numTrainers, -1)
		if result == 0 {
			tm.activeTiles.Delete(toRemove[i])
			log.Warnf("Disabling tile %d", toRemove[i])
		}
	}

	for i := range toAdd {
		_, ok := tm.activeTiles.Load(toAdd[i])
		if !ok {
			tm.activateTileLock.Lock()

			_, ok = tm.activeTiles.Load(toAdd[i])
			if ok {
				trainerNrsValue, ok := tm.activeTileTrainerNumber.Load(toRemove)
				if !ok {
					panic("existing tile did not have a trainers counter")
				}

				numTrainers := trainerNrsValue.(activeTileTrainerNrValueType)
				atomic.AddInt32(numTrainers, 1)

				tm.activateTileLock.Unlock()
				continue
			}

			topLeft, botRight := tm.GetTileBoundsFromTileNr(toAdd[i])
			tile := &Tile{
				pokemons:       sync.Map{},
				BotRightCorner: botRight,
				TopLeftCorner:  topLeft,
			}
			numTrainers := 1
			tm.activeTileTrainerNumber.Store(toAdd[i], &numTrainers)
			tm.activeTiles.Store(toAdd[i], tile)
			go tm.generateWildPokemonsForZonePeriodically(toAdd[i])

			tm.activateTileLock.Unlock()
			continue
		} else {

			trainerNrsValue, ok := tm.activeTileTrainerNumber.Load(toRemove)
			if !ok {
				log.Warn("existing tile did not have a trainers counter")
				continue
			}

			numTrainers := trainerNrsValue.(activeTileTrainerNrValueType)
			atomic.AddInt32(numTrainers, 1)
		}
	}

	tm.trainerTiles.Store(trainerId, currentTiles)
	return currentTiles, changed
}

func (tm *TileManager) calculateLocationTileChanges(trainerId string, row, column int) (toRemove, toAdd,
	currentTiles []int) {
	exitTileNrs := tm.getBoundaryTiles(row, column, tm.exitBoundarySize)

	tileNrsValue, ok := tm.trainerTiles.Load(trainerId)
	if !ok {
		return nil, exitTileNrs, exitTileNrs
	}

	tileNrs := tileNrsValue.(trainerTilesValueType)

	for i := range tileNrs {
		remove := true
		for j := range exitTileNrs {
			if tileNrs[i] == exitTileNrs[j] {
				remove = false
				currentTiles = append(currentTiles, tileNrs[i])
				break
			}
		}

		if remove {
			toRemove = append(toRemove, tileNrs[i])
		}
	}

	entryTileNrs := tm.getBoundaryTiles(row, column, tm.entryBoundarySize)
	for i := range entryTileNrs {
		add := true
		for j := range tileNrs {
			if tileNrs[j] == entryTileNrs[i] {
				add = false
				break
			}
		}

		if add {
			toAdd = append(toAdd, entryTileNrs[i])
			currentTiles = append(currentTiles, entryTileNrs[i])
		}
	}

	return toRemove, toAdd, currentTiles
}

func (tm *TileManager) getBoundaryTiles(row, column int, boundarySize int) []int {
	boundaryRect := tm.CalculateBoundaryForLocation(row, column, boundarySize)

	numTiles := (boundarySize*2 + 1) * (boundarySize*2 + 1)
	tileNrs := make([]int, numTiles)

	minI := int(boundaryRect.Y.Lo)
	minJ := int(boundaryRect.X.Lo)
	maxJ := int(boundaryRect.X.Hi)
	rectSide := maxJ - minJ + 1

	for i := 0; i < rectSide; i++ {
		for j := 0; j < rectSide; j++ {
			iAdjusted := (minI + i) % tm.numTilesPerAxis
			jAdjusted := (minJ + j) % tm.numTilesPerAxis
			tileNrs[j+i*rectSide] = iAdjusted + jAdjusted*tm.numTilesPerAxis
		}
	}

	return tileNrs
}

func (tm *TileManager) CalculateBoundaryForLocation(row, column int, boundarySize int) r2.Rect {
	topLeft := r2.Point{
		X: float64(row - boundarySize),
		Y: float64(column - boundarySize),
	}

	botRight := r2.Point{
		X: float64(row + boundarySize),
		Y: float64(column + boundarySize),
	}

	return r2.RectFromPoints(topLeft, botRight)
}

func (tm *TileManager) LoadGyms(gyms []utils.GymWithServer) {
	for _, gymWithSrv := range gyms {
		gym := gymWithSrv.Gym
		if tm.isWithinBounds(gym.Location) {
			tileNr, _, _, err := tm.GetTileNrFromLocation(gym.Location)
			if err != nil {
				log.Error(err)
				continue
			}
			gymsInterface, ok := tm.gymsFromTile.Load(tileNr)
			var gyms gymsFromTileValueType
			if !ok {
				gyms = gymsFromTileValueType{}
			} else {
				gyms = gymsInterface.(gymsFromTileValueType)
			}
			gyms = append(gyms, gymWithSrv)
			tm.gymsFromTile.LoadOrStore(tileNr, gyms)
		} else {
			log.Infof("Gym %s out of bounds", gym.Name)
		}
	}

	tm.gymsFromTile.Range(func(tileNr, gyms interface{}) bool {
		log.Infof("Tile %d gyms: %+v", tileNr, gyms)
		return true
	})
}

func (tm *TileManager) isWithinBounds(location utils.Location) bool {
	tm.boundariesLock.RLock()
	defer tm.boundariesLock.RUnlock()

	_, row, col, err := tm.GetTileNrFromLocation(location)
	if err != nil {
		return false
	}

	locationPoint := r2.Point{
		X: float64(row),
		Y: float64(col),
	}

	return tm.serverRect.ContainsPoint(locationPoint)
}

func (tm *TileManager) logTileManagerState() {
	numUsers := 0
	tm.trainerTiles.Range(func(_, _ interface{}) bool {
		numUsers++
		return true
	})
	log.Infof("Number of active users: %d", numUsers)
	// log.Info(tm.trainerTiles)
	counter := 0

	tm.activeTiles.Range(func(tileNr, tileInterface interface{}) bool {
		tile := tileInterface.(activeTileValueType)
		counter++
		log.Infof("---------------------Tile %d---------------------", tileNr)
		log.Infof("Tile bounds TopLeft:%+v, TopRight:%+v", tile.TopLeftCorner, tile.BotRightCorner)

		numTrainersValue, ok := tm.activeTileTrainerNumber.Load(tileNr)
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

func (tm *TileManager) AddGym(gymWithSrv utils.GymWithServer) error {
	gym := gymWithSrv.Gym
	tileNr, _, _, err := tm.GetTileNrFromLocation(gym.Location)
	if err != nil {
		log.Error(err)
		return err
	}
	gymsInterface, ok := tm.gymsFromTile.Load(tileNr)
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
	tm.gymsFromTile.Store(tileNr, append(gyms, gymWithSrv))
	return nil
}

func (tm *TileManager) SetBoundaries(topLeftCorner utils.Location, botRightCorner utils.Location) {
	// TODO what to do to clients who become out of region
	tm.boundariesLock.Lock()
	tm.serverRect = locationUtils.LocationsToRect(topLeftCorner, botRightCorner)
	tm.boundariesLock.Unlock()

}

func (tm *TileManager) SetGyms(gymWithSrv []utils.GymWithServer) error {
	for _, gymWithSrv := range gymWithSrv {
		if tm.isWithinBounds(gymWithSrv.Gym.Location) {
			if err := tm.AddGym(gymWithSrv); err != nil {
				return err
			}
		} else {
			continue
		}
	}
	return nil
}

func (tm *TileManager) GetTileBoundsFromTileNr(tileNr int) (topLeft utils.Location, botRight utils.Location) {
	topLeftPoint := r2.Point{
		X: (float64(tileNr%tm.numTilesPerAxis))*tm.tileSideLength - 180.0,
		Y: 180 - float64(tileNr)/float64(tm.numTilesPerAxis)*tm.tileSideLength,
	}

	botRightPoint := r2.Point{
		X: topLeft.Longitude + tm.tileSideLength,
		Y: topLeft.Latitude - tm.tileSideLength,
	}

	proj := s2.NewMercatorProjection(180)
	topLeftLatLng := proj.ToLatLng(topLeftPoint)
	botRightLatLng := proj.ToLatLng(botRightPoint)

	topLeft = utils.Location{
		Latitude:  topLeftLatLng.Lat.Degrees(),
		Longitude: topLeftLatLng.Lng.Degrees(),
	}

	botRight = utils.Location{
		Latitude:  botRightLatLng.Lat.Degrees(),
		Longitude: botRightLatLng.Lng.Degrees(),
	}

	return topLeft, botRight
}

func (tm *TileManager) GetTileCenterLocationFromTileNr(tileNr int) utils.Location {
	topLeft, _ := tm.GetTileBoundsFromTileNr(tileNr)

	return utils.Location{
		Latitude:  topLeft.Latitude - (tm.tileSideLength / 2),
		Longitude: topLeft.Longitude + (tm.tileSideLength / 2),
	}
}
