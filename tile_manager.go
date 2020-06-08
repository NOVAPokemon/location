package main

import (
	"errors"
	"fmt"
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
	activeTileValueType   = *Tile
	trainerTileValueType  = int
)

type TileManager struct {
	pokemonSpecies           []string
	NumTilesInWorld          int
	TopLeftCorner            utils.Location
	BotRightCorner           utils.Location
	gymsFromTile             sync.Map
	activeTiles              sync.Map
	trainerTile              sync.Map
	numTilesPerAxis          int
	tileSideLength           int
	maxPokemonsPerTile       int
	maxPokemonsPerGeneration int
}

type Tile struct {
	pokemonLock    sync.RWMutex
	nrTrainers     int
	borderTile     bool
	pokemons       []utils.WildPokemon
	TopLeftCorner  utils.Location
	BotRightCorner utils.Location
}

const LatitudeMax = 85.05115

var gymsLock sync.RWMutex

func NewTileManager(gyms []utils.GymWithServer, numTiles int, maxPokemonsPerTile int, pokemonsPerGeneration int,
	topLeft utils.Location, botRight utils.Location) *TileManager {

	numTilesPerAxis := int(math.Sqrt(float64(numTiles)))
	tileSide := 360.0 / numTilesPerAxis
	toReturn := &TileManager{
		NumTilesInWorld:          numTiles,
		TopLeftCorner:            topLeft,
		BotRightCorner:           botRight,
		activeTiles:              sync.Map{},
		trainerTile:              sync.Map{},
		numTilesPerAxis:          numTilesPerAxis,
		tileSideLength:           tileSide,
		maxPokemonsPerTile:       maxPokemonsPerTile,
		maxPokemonsPerGeneration: pokemonsPerGeneration,
	}
	toReturn.LoadGyms(gyms)
	return toReturn
}

func (tm *TileManager) SetTrainerLocation(trainerId string, location utils.Location) (int, error) {
	tm.logTileManagerState()

	if !isWithinBounds(location, tm.TopLeftCorner, tm.BotRightCorner) {
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
			logrus.Infof("Trainer %s is still in the same tile (%d)", trainerId, tileNr)
			return tileNr, nil
		}
	}

	tileInterface, ok := tm.activeTiles.Load(tileNr)
	if ok {
		tile := tileInterface.(activeTileValueType)
		log.Infof("Trainer joined an already created zone (%d)", tileNr)
		if !trainerRegistered {
			tile.nrTrainers++
		}
	} else {
		// no tile initialized for user
		logrus.Infof("Created new tile (%d) for trainer: %s", tileNr, trainerId)
		topLeft, botRight := tm.GetTileBoundsFromTileNr(tileNr)
		tile := &Tile{
			nrTrainers:     1,
			pokemons:       make([]utils.WildPokemon, 0, tm.maxPokemonsPerTile),
			borderTile:     tm.isBorderTile(topLeft, botRight),
			BotRightCorner: botRight,
			TopLeftCorner:  topLeft,
		}
		tm.activeTiles.Store(tileNr, tile)
		go tm.generateWildPokemonsForZonePeriodically(tileNr)
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
		tile.nrTrainers--
		if tile.nrTrainers == 0 {
			log.Warnf("Disabling tile %d", tileNr)
			tm.activeTiles.Delete(tileNr)
		}
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
	pokemonsLock := &tile.pokemonLock

	defer pokemonsLock.RUnlock()
	pokemonsLock.RLock()
	pokemonsClone := tile.pokemons
	return pokemonsClone, nil
}

func (tm *TileManager) getGymsInTile(tileNr int) []utils.GymWithServer {
	gymsLock.RLock()
	gymsInTileInterface, ok := tm.gymsFromTile.Load(tileNr)
	gymsLock.RUnlock()

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
	tileInterface, ok := tm.activeTiles.Load(zoneNr)
	if !ok {
		log.Warnf("Stopped generating pokemons for zone %d due to missing zone", zoneNr)
		return
	}

	tile := tileInterface.(activeTileValueType)
	for ; ok && tile.nrTrainers != 0; {
		log.Info("Refreshing wild pokemons...")
		pokemonLock := &tile.pokemonLock
		pokemonLock.Lock()
		nrToGenerate := tm.maxPokemonsPerTile - len(tile.pokemons)
		if nrToGenerate > tm.maxPokemonsPerGeneration {
			nrToGenerate = tm.maxPokemonsPerGeneration
		}
		wildPokemons := generateWildPokemons(nrToGenerate, pokemonSpecies, tile.TopLeftCorner, tile.BotRightCorner)
		tile.pokemons = append(tile.pokemons, wildPokemons...)
		pokemonLock.Unlock()
		log.Infof("Added %d pokemons to zone %d", len(wildPokemons), zoneNr)
		time.Sleep(time.Duration(config.IntervalBetweenGenerations) * time.Second)
	}
	log.Warnf("Stopped generating pokemons for zone %d", zoneNr)
}

func (tm *TileManager) cleanWildPokemons(tileNr int) {
	tileInterface, ok := tm.activeTiles.Load(tileNr)
	if ok {
		tile := tileInterface.(activeTileValueType)
		tile.pokemons = make([]utils.WildPokemon, 0)
	}
}

func (tm *TileManager) RemoveWildPokemonFromTile(tileNr int, pokemonId string) (*pokemons.Pokemon, error) {
	tileInterface, ok := tm.activeTiles.Load(tileNr)
	if !ok {
		return nil, errors.New("tile is nil")
	}

	tile := tileInterface.(activeTileValueType)
	pokemonLock := &tile.pokemonLock
	pokemonLock.Lock()
	defer pokemonLock.Unlock()

	found := false
	var pokemon *pokemons.Pokemon
	for i, wp := range tile.pokemons {
		if wp.Pokemon.Id.Hex() == pokemonId {
			found = true
			pokemon = &tile.pokemons[i].Pokemon
			tile.pokemons = append(tile.pokemons[:i], tile.pokemons[i+1:]...)
			break
		}
	}
	if !found {
		return nil, errors.New("pokemon not found")
	}
	return pokemon, nil
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
	tm.gymsFromTile = sync.Map{}

	for _, gymWithSrv := range gyms {
		gym := gymWithSrv.Gym
		if isWithinBounds(gym.Location, tm.TopLeftCorner, tm.BotRightCorner) {
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

			tm.gymsFromTile.Store(tileNr, append(gyms, gymWithSrv))
		} else {
			log.Infof("Gym %s out of bounds", gym.Name)
		}
	}

	tm.gymsFromTile.Range(func(tileNr, gyms interface{}) bool {
		log.Infof("Tile %d gyms: %+v", tileNr, gyms)
		return true
	})
}

func isWithinBounds(location utils.Location, topLeft utils.Location, botRight utils.Location) bool {
	if location.Longitude >= botRight.Longitude || location.Longitude <= topLeft.Longitude {
		return false
	}

	if location.Latitude <= botRight.Latitude || location.Latitude >= topLeft.Latitude {
		return false
	}
	return true
}

func (tm *TileManager) isBorderTile(topLeft utils.Location, botRight utils.Location) bool {
	return topLeft.Longitude == tm.TopLeftCorner.Longitude || topLeft.Latitude == tm.TopLeftCorner.Latitude ||
		botRight.Latitude == tm.TopLeftCorner.Latitude || botRight.Longitude == tm.BotRightCorner.Longitude
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
		log.Info("Number of active users: ", tile.nrTrainers)
		log.Info("Number of generated pokemons: ", len(tile.pokemons))

		gymsInterface, _ := tm.gymsFromTile.Load(tileNr)
		gyms := gymsInterface.(gymsFromTileValueType)

		log.Info("Number of gyms: ", len(gymsInterface.(gymsFromTileValueType)))

		// for i, pokemon := range tile.pokemons {
		//	log.Infof("Wild pokemon %d location: %+v", i, pokemon.Location)
		// }

		for _, gymWithSrv := range gyms {
			log.Infof("Gym %s location: %+v", gymWithSrv.Gym.Name, gymWithSrv.Gym.Location)
		}

		return true
	})

	log.Infof("Number of active tiles: %d", counter)
}

func (tm *TileManager) AddGym(gymWithSrv utils.GymWithServer) error {
	gym := gymWithSrv.Gym
	if isWithinBounds(gym.Location, tm.TopLeftCorner, tm.BotRightCorner) {
		log.Infof("Adding gym %s", gym.Name)
		tileNr, err := GetTileNrFromLocation(gym.Location, tm.numTilesPerAxis, tm.tileSideLength)
		if err != nil {
			log.Error(err)
			return err
		}

		gymsInterface, _ := tm.gymsFromTile.Load(tileNr)
		gyms := gymsInterface.(gymsFromTileValueType)
		tm.gymsFromTile.Store(tileNr, append(gyms, gymWithSrv))
		return nil
	} else {
		return errors.New(fmt.Sprintf("Gym %s out of bounds", gym.Name))
	}
}

func (tm *TileManager) SetBoundaries(topLeftCorner utils.Location, botRightCorner utils.Location) {
	// TODO what to do to clients who become out of region
	tm.BotRightCorner = botRightCorner
	tm.TopLeftCorner = topLeftCorner
}
