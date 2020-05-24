package main

import (
	"errors"
	"fmt"
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/pokemons"
	"github.com/golang/geo/s2"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"math"
	"sync"
	"time"
)

type TileManager struct {
	pokemonSpecies           []string
	NumTilesInWorld          int
	TopLeftCorner            utils.Location
	BotRightCorner           utils.Location
	gymsFromTile             map[int][]utils.Gym
	activeTiles              map[int]*Tile
	trainerTile              map[string]int
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

func NewTileManager(gyms []utils.Gym, numTiles int, maxPokemonsPerTile int, pokemonsPerGeneration int, topLeft utils.Location, botRight utils.Location) *TileManager {

	numTilesPerAxis := int(math.Sqrt(float64(numTiles)))
	tileSide := 360.0 / numTilesPerAxis
	toReturn := &TileManager{
		NumTilesInWorld:          numTiles,
		TopLeftCorner:            topLeft,
		BotRightCorner:           botRight,
		activeTiles:              make(map[int]*Tile),
		trainerTile:              make(map[string]int),
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

	lastTile, trainerRegistered := tm.trainerTile[trainerId]
	if trainerRegistered {
		if lastTile == tileNr {
			//user remained in the same tile, no need to check if tile exists because tiles cant be deleted with
			// a user there
			logrus.Infof("Trainer %s is still in the same tile (%d)", trainerId, tileNr)
			return tileNr, nil
		}
	}

	tile, ok := tm.activeTiles[tileNr]

	if ok {
		log.Infof("Trainer joined an already created zone (%d)", tileNr)
		if !trainerRegistered {
			tile.nrTrainers++
		}
	} else {
		// no tile initialized for user
		logrus.Infof("Created new tile (%d) for trainer: %s", tileNr, trainerId)
		topLeft, botRight := tm.GetTileBoundsFromTileNr(tileNr)
		tile = &Tile{
			nrTrainers:     1,
			pokemons:       make([]utils.WildPokemon, 0, tm.maxPokemonsPerTile),
			borderTile:     tm.isBorderTile(topLeft, botRight),
			BotRightCorner: botRight,
			TopLeftCorner:  topLeft,
		}
		tm.activeTiles[tileNr] = tile
		go tm.generateWildPokemonsForZonePeriodically(tileNr)
	}

	tm.trainerTile[trainerId] = tileNr
	return tileNr, nil
}

func (tm *TileManager) RemoveTrainerLocation(trainerId string) error {
	tileNr, ok := tm.trainerTile[trainerId]
	if !ok {
		return errors.New("user was not being tracked")
	}

	if tile, ok := tm.activeTiles[tileNr]; ok {
		tile.nrTrainers--
		if tile.nrTrainers == 0 {
			log.Warnf("Disabling tile %d", tileNr)
			delete(tm.activeTiles, tileNr)
		}
	}
	delete(tm.trainerTile, trainerId)
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

func (tm *TileManager) GetTrainerTile(trainerId string) (int, bool) {
	tileNr, ok := tm.trainerTile[trainerId]
	return tileNr, ok
}

func (tm *TileManager) getPokemonsInTile(tileNr int) ([]utils.WildPokemon, error) {
	tile, ok := tm.activeTiles[tileNr]
	if !ok {
		return nil, errors.New("tile is nil")
	}
	pokemonsLock := &tile.pokemonLock

	defer pokemonsLock.RUnlock()
	pokemonsLock.RLock()
	pokemonsClone := tile.pokemons
	return pokemonsClone, nil
}

func (tm *TileManager) getGymsInTile(tileNr int) []utils.Gym {
	gymsLock.RLock()
	gymsInTile, ok := tm.gymsFromTile[tileNr]
	gymsLock.RUnlock()

	if !ok {
		return []utils.Gym{}
	}
	/*
		var gymsInVicinity []utils.Gym
			for _, gym := range gymsInTile {
				distance := gps.CalcDistanceBetweenLocations(location, gym.Location)
				if distance <= config.Vicinity {
					gymsInVicinity = append(gymsInVicinity, gym)
				}
			}*/

	return gymsInTile
}

func (tm *TileManager) generateWildPokemonsForZonePeriodically(zoneNr int) {
	for tile, ok := tm.activeTiles[zoneNr]; ok && tile.nrTrainers != 0; {
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
	tile, ok := tm.activeTiles[tileNr]
	if ok {
		tile.pokemons = make([]utils.WildPokemon, 0)
	}
}

func (tm *TileManager) RemoveWildPokemonFromTile(tileNr int, pokemonId string) (*pokemons.Pokemon, error) {
	tile, ok := tm.activeTiles[tileNr]
	if !ok {
		return nil, errors.New("tile is nil")
	}
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

func (tm *TileManager) LoadGyms(gyms []utils.Gym) {

	tm.gymsFromTile = make(map[int][]utils.Gym, len(gyms))

	for _, gym := range gyms {

		if isWithinBounds(gym.Location, tm.TopLeftCorner, tm.BotRightCorner) {
			tileNr, err := GetTileNrFromLocation(gym.Location, tm.numTilesPerAxis, tm.tileSideLength)
			if err != nil {
				log.Error(err)
				continue
			}
			tm.gymsFromTile[tileNr] = append(tm.gymsFromTile[tileNr], gym)
		} else {
			log.Infof("Gym %s out of bounds", gym.Name)
		}
	}

	for tileNr, gyms := range tm.gymsFromTile {
		log.Infof("Tile %d gyms: %+v", tileNr, gyms)
	}
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
	log.Infof("Number of active tiles: %d", len(tm.activeTiles))
	log.Infof("Number of active users: %d", len(tm.trainerTile))
	log.Info(tm.trainerTile)
	for tileNr, tile := range tm.activeTiles {
		log.Infof("---------------------Tile %d---------------------", tileNr)
		log.Infof("Tile bounds TopLeft:%+v, TopRight:%+v", tile.TopLeftCorner, tile.BotRightCorner)
		log.Info("Number of active users: ", tile.nrTrainers)
		log.Info("Number of generated pokemons: ", len(tile.pokemons))
		log.Info("Number of gyms: ", len(tm.gymsFromTile[tileNr]))

		//for i, pokemon := range tile.pokemons {
		//	log.Infof("Wild pokemon %d location: %+v", i, pokemon.Location)
		//}

		for _, gym := range tm.gymsFromTile[tileNr] {
			log.Infof("Gym %s location: %+v", gym.Name, gym.Location)
		}

	}
}

func (tm *TileManager) AddGym(gym utils.Gym) error {
	if isWithinBounds(gym.Location, tm.TopLeftCorner, tm.BotRightCorner) {
		log.Infof("Adding gym %s", gym.Name)
		tileNr, err := GetTileNrFromLocation(gym.Location, tm.numTilesPerAxis, tm.tileSideLength)
		if err != nil {
			log.Error(err)
			return err
		}
		tm.gymsFromTile[tileNr] = append(tm.gymsFromTile[tileNr], gym)
		return nil
	} else {
		return errors.New(fmt.Sprintf("Gym %s out of bounds", gym.Name))
	}
}

func (tm *TileManager) SetBoundaries(topLeftCorner utils.Location, botRightCorner utils.Location) {
	//TODO what to do to clients who become out of region
	tm.BotRightCorner = botRightCorner
	tm.TopLeftCorner = topLeftCorner
}
