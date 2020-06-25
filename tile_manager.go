package main

import (
	"errors"
	"math"
	"sync"
	"time"

	"github.com/NOVAPokemon/utils"
	locationUtils "github.com/NOVAPokemon/utils/location"
	"github.com/NOVAPokemon/utils/pokemons"
	"github.com/golang/geo/s2"
	log "github.com/sirupsen/logrus"
)

type (
	gymsFromTileValueType = []utils.GymWithServer
	activeTileValueType = *Tile
	trainerTilesValueType = []int
)

type TileManager struct {
	pokemonSpecies           []string
	NumTilesInWorld          int
	boundariesLock           sync.RWMutex
	boundary                 utils.Boundary
	gymsFromTile             sync.Map
	activeTiles              sync.Map
	trainerTiles             sync.Map
	numTilesPerAxis          int
	tileSideLength           float64
	maxPokemonsPerTile       int
	maxPokemonsPerGeneration int
	entryBoundarySize        float64
	exitBoundarySize         float64
	activateTileLock         sync.Mutex
}

type Tile struct {
	nrTrainerMutex sync.RWMutex
	nrTrainers     int
	pokemons       sync.Map
	TopLeftCorner  utils.Location
	BotRightCorner utils.Location
}

const LatitudeMax = 85.05115

func NewTileManager(gyms []utils.GymWithServer, numTiles, maxPokemonsPerTile, pokemonsPerGeneration int,
	topLeft utils.Location, botRight utils.Location) *TileManager {
	numTilesPerAxis := int(math.Sqrt(float64(numTiles)))
	tileSide := 360.0 / float64(numTilesPerAxis)
	toReturn := &TileManager{
		NumTilesInWorld: numTiles,
		boundary: utils.Boundary{
			TopLeft:     topLeft,
			BottomRight: botRight,
		},
		activeTiles:              sync.Map{},
		trainerTiles:             sync.Map{},
		numTilesPerAxis:          numTilesPerAxis,
		tileSideLength:           tileSide,
		maxPokemonsPerTile:       maxPokemonsPerTile,
		maxPokemonsPerGeneration: pokemonsPerGeneration,
		activateTileLock:         sync.Mutex{},
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

	tileNr := tileNrInterface.(trainerTilesValueType)
	if tileInterface, ok := tm.activeTiles.Load(tileNr); ok {
		tile := tileInterface.(activeTileValueType)
		tile.nrTrainerMutex.Lock()
		tile.nrTrainers--
		if tile.nrTrainers == 0 {
			log.Warnf("Disabling tile %d", tileNr)
			tm.activeTiles.Delete(tileNr)
		}
		tile.nrTrainerMutex.Unlock()
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

func (tm *TileManager) UpdateTrainerTiles(trainerId string, location utils.Location) ([]int, bool, error) {
	toRemove, toAdd, currentTiles, err := tm.calculateLocationTileChanges(trainerId, location)
	if err != nil {
		return nil, false, err
	}

	changed := len(toRemove) > 0 || len(toAdd) > 0

	for i := range toRemove {
		tileValue, ok := tm.activeTiles.Load(toRemove[i])
		if !ok {
			continue
		}

		tile := tileValue.(activeTileValueType)
		tile.nrTrainerMutex.Lock()
		tile.nrTrainers--
		if tile.nrTrainers == 0 {
			tm.activeTiles.Delete(toRemove[i])
			log.Warnf("Disabling tile %d", toRemove[i])
		}
		tile.nrTrainerMutex.Unlock()
	}

	for i := range toAdd {
		tileValue, ok := tm.activeTiles.Load(toAdd[i])
		if !ok {
			tm.activateTileLock.Lock()

			_, ok = tm.activeTiles.Load(toAdd[i])
			if ok {
				tm.activateTileLock.Unlock()
				continue
			}

			topLeft, botRight := tm.GetTileBoundsFromTileNr(toAdd[i])
			tile := &Tile{
				nrTrainers:     1,
				pokemons:       sync.Map{},
				BotRightCorner: botRight,
				TopLeftCorner:  topLeft,
			}
			tm.activeTiles.Store(toAdd[i], tile)
			go tm.generateWildPokemonsForZonePeriodically(toAdd[i])

			tm.activateTileLock.Unlock()
			continue
		} else {
			tile := tileValue.(activeTileValueType)
			tile.nrTrainerMutex.Lock()
			tile.nrTrainers++
			tile.nrTrainerMutex.Unlock()
		}
	}

	tm.trainerTiles.Store(trainerId, currentTiles)
	return currentTiles, changed, nil
}

func (tm *TileManager) calculateLocationTileChanges(trainerId string, location utils.Location) (toRemove, toAdd,
	currentTiles []int, err error) {
	exitTileNrs, err := tm.getBoundaryTiles(location, tm.exitBoundarySize)
	if err != nil {
		return nil, nil, nil, err
	}

	tileNrsValue, ok := tm.trainerTiles.Load(trainerId)
	if !ok {
		return nil, nil, nil, errors.New("trainer hasnt been added yet")
	}

	tileNrs := tileNrsValue.(trainerTilesValueType)
	for i := range tileNrs {
		remove := true
		for j := range exitTileNrs {
			if tileNrs[i] == exitTileNrs[j] {
				remove = false
			}
		}

		if remove {
			toRemove = append(toRemove, tileNrs[i])
		}
	}

	entryTileNrs, err := tm.getBoundaryTiles(location, tm.entryBoundarySize)
	if err != nil {
		return nil, nil, nil, err
	}

	for i := range entryTileNrs {
		add := true
		for j := range tileNrs {
			if tileNrs[j] == entryTileNrs[i] {
				add = false
			}
		}

		if add {
			toAdd = append(toAdd, entryTileNrs[i])
		}
	}

	return toRemove, toAdd, exitTileNrs, nil
}

func (tm *TileManager) getBoundaryTiles(location utils.Location, boundarySize float64) ([]int, error) {
	// CALCULATE TILES WHICH ARE PART OF MY EXIT NEIGHBORHOOD
	boundary := tm.CalculateBoundaryForLocation(location, boundarySize)

	_, topBoundaryRow, topBoundaryCol, err := tm.GetTileNrFromLocation(boundary.TopLeft)
	if err != nil {
		return nil, err
	}

	_, bottomBoundaryRow, bottomBoundaryCol, err := tm.GetTileNrFromLocation(boundary.BottomRight)
	if err != nil {
		return nil, err
	}

	verticalNeighborhoodSize := int(math.Abs(float64(topBoundaryCol-bottomBoundaryCol))) + 1
	horizontalNeighborhoodSize := int(math.Abs(float64(bottomBoundaryRow-topBoundaryRow))) + 1
	numTiles := verticalNeighborhoodSize * horizontalNeighborhoodSize
	tileNrs := make([]int, numTiles)

	for j := topBoundaryCol; j <= bottomBoundaryCol; j++ {
		for i := topBoundaryRow; i <= bottomBoundaryRow; i++ {
			iAdjusted := (topBoundaryRow + i) % tm.numTilesPerAxis
			jAdjusted := (topBoundaryRow + j) % tm.numTilesPerAxis
			tileNrs[i+j*horizontalNeighborhoodSize] = iAdjusted + jAdjusted*tm.numTilesPerAxis
		}
	}

	return tileNrs, nil
}

func (tm *TileManager) CalculateBoundaryForLocation(location utils.Location, boundarySize float64) utils.Boundary {
	return utils.Boundary{
		TopLeft: utils.Location{
			Latitude:  location.Latitude + (boundarySize * tm.tileSideLength),
			Longitude: location.Longitude - (boundarySize * tm.tileSideLength),
		},
		BottomRight: utils.Location{
			Latitude:  location.Latitude - (boundarySize * tm.tileSideLength),
			Longitude: location.Longitude + (boundarySize * tm.tileSideLength),
		},
	}
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
	res := locationUtils.IsWithinBounds(location, tm.boundary.TopLeft, tm.boundary.BottomRight)
	tm.boundariesLock.RUnlock()
	return res
}

func (tm *TileManager) isBorderTile(topLeft utils.Location, botRight utils.Location) bool {
	tm.boundariesLock.RLock()
	res := topLeft.Longitude == tm.boundary.TopLeft.Longitude || topLeft.Latitude == tm.boundary.TopLeft.Latitude ||
		botRight.Latitude == tm.boundary.TopLeft.Latitude || botRight.Longitude == tm.boundary.BottomRight.Longitude
	tm.boundariesLock.RUnlock()
	return res
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
		tile.nrTrainerMutex.RLock()
		log.Info("Number of active users: ", tile.nrTrainers)
		tile.nrTrainerMutex.RUnlock()
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
	tm.boundary.BottomRight = botRightCorner
	tm.boundary.TopLeft = topLeftCorner
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
	topLeft = utils.Location{
		Longitude: (float64(tileNr % tm.numTilesPerAxis))*tm.tileSideLength - 180.0,
		Latitude:  180 - float64(tileNr)/float64(tm.numTilesPerAxis)*tm.tileSideLength,
	}

	botRight = utils.Location{
		Latitude:  topLeft.Latitude - tm.tileSideLength,
		Longitude: topLeft.Longitude + tm.tileSideLength,
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
