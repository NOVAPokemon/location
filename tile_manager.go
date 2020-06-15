package main

import (
	"errors"
	"math"
	"sync"
	"time"

	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/pokemons"
	"github.com/golang/geo/s2"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
)

type (
	gymsFromTileValueType = []utils.GymWithServer
	activeTileValueType = *Tile
	trainerTileValueType = int
)

type TileManager struct {
	pokemonSpecies           []string
	NumTilesInWorld          int
	boundariesLock           sync.RWMutex
	topLeftCorner            utils.Location
	botRightCorner           utils.Location
	gymsFromTile             sync.Map
	activeTiles              sync.Map
	trainerTile              sync.Map
	numTilesPerAxis          int
	tileSideLength           int
	maxPokemonsPerTile       int
	maxPokemonsPerGeneration int
}

type Tile struct {
	nrTrainerMutex sync.RWMutex
	nrTrainers     int
	borderTile     bool
	pokemons       sync.Map
	TopLeftCorner  utils.Location
	BotRightCorner utils.Location
}

const LatitudeMax = 85.05115

func NewTileManager(gyms []utils.GymWithServer, numTiles int, maxPokemonsPerTile int, pokemonsPerGeneration int,
	topLeft utils.Location, botRight utils.Location) *TileManager {

	numTilesPerAxis := int(math.Sqrt(float64(numTiles)))
	tileSide := 360.0 / numTilesPerAxis
	toReturn := &TileManager{
		NumTilesInWorld:          numTiles,
		topLeftCorner:            topLeft,
		botRightCorner:           botRight,
		activeTiles:              sync.Map{},
		trainerTile:              sync.Map{},
		numTilesPerAxis:          numTilesPerAxis,
		tileSideLength:           tileSide,
		maxPokemonsPerTile:       maxPokemonsPerTile,
		maxPokemonsPerGeneration: pokemonsPerGeneration,
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

func (tm *TileManager) SetTrainerLocation(trainerId string, location utils.Location) (int, error) {
	if !tm.isWithinBounds(location) {
		return -1, errors.New("out of bounds of the server")
	}
	tileNr, err := GetTileNrFromLocation(location, tm.numTilesPerAxis, tm.tileSideLength)
	if err != nil {
		log.Error(err)
		return -1, err
	}
	lastTileInterface, trainerRegistered := tm.trainerTile.Load(trainerId)
	if trainerRegistered {
		lastTile := lastTileInterface.(trainerTileValueType)
		if lastTile == tileNr {
			// user remained in the same tile, no need to check if tile exists because tiles cant be deleted with
			// a user there
			// logrus.Infof("Trainer %s is still in the same tile (%d)", trainerId, tileNr)
			return tileNr, nil
		}
	}

	tileInterface, ok := tm.activeTiles.Load(tileNr)
	if ok {
		tile := tileInterface.(activeTileValueType)
		log.Infof("Trainer joined an already created zone (%d)", tileNr)
		if !trainerRegistered {
			tile.nrTrainerMutex.Lock()
			tile.nrTrainers++
			tile.nrTrainerMutex.Unlock()
		}
		tm.logTileManagerState()
	} else {
		// no tile initialized for user
		logrus.Infof("Created new tile (%d) for trainer: %s", tileNr, trainerId)
		topLeft, botRight := tm.GetTileBoundsFromTileNr(tileNr)
		tile := &Tile{
			nrTrainers:     1,
			pokemons:       sync.Map{},
			borderTile:     tm.isBorderTile(topLeft, botRight),
			BotRightCorner: botRight,
			TopLeftCorner:  topLeft,
		}
		tm.activeTiles.Store(tileNr, tile)
		go tm.generateWildPokemonsForZonePeriodically(tileNr)
		tm.logTileManagerState()
	}

	tm.trainerTile.Store(trainerId, tileNr)
	return tileNr, nil
}

func (tm *TileManager) RemoveTrainerLocation(trainerId string) error {
	tileNrInterface, ok := tm.trainerTile.Load(trainerId)
	if !ok {
		return errors.New("user was not being tracked")
	}

	tileNr := tileNrInterface.(trainerTileValueType)
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
	tm.trainerTile.Delete(trainerId)
	return nil
}

func (tm *TileManager) GetTileBoundsFromTileNr(tileNr int) (topLeft utils.Location, botRight utils.Location) {

	topLeft = utils.Location{
		Longitude: float64((tileNr%tm.numTilesPerAxis)*tm.tileSideLength) - 180,
		Latitude:  180 - float64(tileNr/tm.numTilesPerAxis*tm.tileSideLength),
	}

	botRight = utils.Location{
		Latitude:  topLeft.Latitude - float64(tm.tileSideLength),
		Longitude: topLeft.Longitude + float64(tm.tileSideLength),
	}

	return topLeft, botRight
}

func (tm *TileManager) GetTrainerTile(trainerId string) (interface{}, bool) {
	tileNrInterface, ok := tm.trainerTile.Load(trainerId)
	return tileNrInterface, ok
}

func (tm *TileManager) getPokemonsInTile(tileNr int) ([]utils.WildPokemon, error) {
	tileInterface, ok := tm.activeTiles.Load(tileNr)
	if !ok {
		return nil, errors.New("tile is nil")
	}

	tile := tileInterface.(activeTileValueType)
	toReturn := make([]utils.WildPokemon, 0)
	tile.pokemons.Range(func(key, value interface{}) bool {
		toReturn = append(toReturn, value.(utils.WildPokemon))
		return true
	})
	return toReturn, nil
}

func (tm *TileManager) getGymsInTile(tileNr int) []utils.GymWithServer {
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
			}*/
	return gymsInTileInterface.(gymsFromTileValueType)
}

func (tm *TileManager) generateWildPokemonsForZonePeriodically(zoneNr int) {
	for {
		tileInterface, ok := tm.activeTiles.Load(zoneNr)
		if !ok {
			log.Warnf("Stopped generating pokemons for zone %d due to missing zone", zoneNr)
			return
		}
		tile := tileInterface.(Tile)

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
		toReturn := value.(utils.WildPokemon).Pokemon
		return &toReturn, nil
	}
}

// auxiliary functions

func GetTileNrFromLocation(location utils.Location, numTilesPerAxis int, tileSideLength int) (int, error) {

	if location.Latitude >= LatitudeMax || location.Latitude <= -LatitudeMax {
		return -1, errors.New("latitude value out of bounds, bound is: -[85.05115 : 85.05115]")
	}
	if location.Longitude >= 179.9 || location.Longitude <= -179.9 {
		return -1, errors.New("latitude value out of bounds, bound is: -[179.9 : 179.9]")
	}

	latLong := s2.LatLngFromDegrees(location.Latitude, location.Longitude)

	proj := s2.NewMercatorProjection(180)
	transformedPoint := proj.FromLatLng(latLong)

	tileCol := int(math.Floor((180 + transformedPoint.X) / float64(tileSideLength)))
	tileRow := int(math.Floor((180 - transformedPoint.Y) / float64(tileSideLength)))
	tileNr := tileRow*numTilesPerAxis + tileCol
	return tileNr, nil
}

func (tm *TileManager) LoadGyms(gyms []utils.GymWithServer) {
	for _, gymWithSrv := range gyms {
		gym := gymWithSrv.Gym
		if tm.isWithinBounds(gym.Location) {
			tileNr, err := GetTileNrFromLocation(gym.Location, tm.numTilesPerAxis, tm.tileSideLength)
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
	return isWithinBounds(location, tm.topLeftCorner, tm.botRightCorner)
}

func (tm *TileManager) isBorderTile(topLeft utils.Location, botRight utils.Location) bool {
	tm.boundariesLock.RLock()
	defer tm.boundariesLock.RUnlock()
	return topLeft.Longitude == tm.topLeftCorner.Longitude || topLeft.Latitude == tm.topLeftCorner.Latitude ||
		botRight.Latitude == tm.topLeftCorner.Latitude || botRight.Longitude == tm.botRightCorner.Longitude
}

func (tm *TileManager) logTileManagerState() {
	numUsers := 0
	tm.trainerTile.Range(func(_, _ interface{}) bool {
		numUsers++
		return true
	})
	log.Infof("Number of active users: %d", numUsers)
	// log.Info(tm.trainerTile)
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
	tileNr, err := GetTileNrFromLocation(gym.Location, tm.numTilesPerAxis, tm.tileSideLength)
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
	tm.boundariesLock.Unlock()
	tm.botRightCorner = botRightCorner
	tm.topLeftCorner = topLeftCorner
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

// auxiliary functions

func isWithinBounds(location utils.Location, topLeft utils.Location, botRight utils.Location) bool {
	if location.Longitude >= botRight.Longitude || location.Longitude <= topLeft.Longitude {
		return false
	}
	if location.Latitude <= botRight.Latitude || location.Latitude >= topLeft.Latitude {
		return false
	}
	return true
}
