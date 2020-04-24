package main

import (
	"errors"
	"github.com/NOVAPokemon/utils"
	locationdb "github.com/NOVAPokemon/utils/database/location"
	"github.com/NOVAPokemon/utils/pokemons"
	"github.com/golang/geo/s2"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"math"
	"sync"
	"time"
)

type RegionManager struct {
	pokemonSpecies           []string
	NumRegionsInWorld        int
	TopLeftCorner            utils.Location
	BotRightCorner           utils.Location
	gymsFromRegion           map[int][]utils.Gym
	activeRegions            map[int]*Region
	trainerRegion            map[string]int
	numRegionsPerAxis        int
	regionSideLength         int
	maxPokemonsPerRegion     int
	maxPokemonsPerGeneration int
}

type Region struct {
	pokemonLock    sync.RWMutex
	nrTrainers     int
	pokemons       []utils.WildPokemon
	TopLeftCorner  utils.Location
	BotRightCorner utils.Location
}

const LatitudeMax = 85.05115

var gymsLock sync.RWMutex

func NewRegionManager(gyms []utils.Gym, numRegions int, maxPokemonsPerRegion int, pokemonsPerGeneration int, topLeft utils.Location, botRight utils.Location) *RegionManager {

	numRegionsPerAxis := int(math.Sqrt(float64(numRegions)))
	regionSide := int(360.0 / numRegionsPerAxis)
	toReturn := &RegionManager{
		NumRegionsInWorld:        numRegions,
		TopLeftCorner:            topLeft,
		BotRightCorner:           botRight,
		activeRegions:            make(map[int]*Region),
		trainerRegion:            make(map[string]int),
		numRegionsPerAxis:        numRegionsPerAxis,
		regionSideLength:         regionSide,
		maxPokemonsPerRegion:     maxPokemonsPerRegion,
		maxPokemonsPerGeneration: pokemonsPerGeneration,
	}
	toReturn.LoadGyms(gyms)
	return toReturn
}

func (rm *RegionManager) SetTrainerLocation(trainerId string, location utils.Location) (int, error) {
	rm.logRegionManagerState()

	if !isWithinBounds(location, rm.TopLeftCorner, rm.BotRightCorner) {
		return -1, errors.New("out of bounds of the server")
	}

	regionNr, err := GetRegionNrFromLocation(location, rm.numRegionsPerAxis, rm.regionSideLength)

	if err != nil {
		log.Error(err)
		return -1, err
	}

	lastRegion, ok := rm.trainerRegion[trainerId]
	if ok {
		if lastRegion == regionNr {
			//user remained in the same region, no need to check if region exists because regions cant be deleted with
			// a user there
			logrus.Infof("Trainer %s is still in the same region (%d)", trainerId, regionNr)
			return regionNr, nil
		}
	}

	region, ok := rm.activeRegions[regionNr]

	if ok {
		log.Infof("Trainer joined an already created zone (%d)", regionNr)
	} else {
		// no region initialized for user
		logrus.Infof("Created new region (%d) for trainer: %s", regionNr, trainerId)
		topLeft, botRight := rm.GetRegionBoundsFromRegionNr(regionNr)
		region = &Region{
			nrTrainers:     1,
			pokemons:       make([]utils.WildPokemon, 0, rm.maxPokemonsPerRegion),
			BotRightCorner: botRight,
			TopLeftCorner:  topLeft,
		}
		rm.activeRegions[regionNr] = region
		go rm.generateWildPokemonsForZonePeriodically(regionNr)
	}
	rm.trainerRegion[trainerId] = regionNr
	return regionNr, nil
}

func (rm *RegionManager) RemoveTrainerLocation(trainerId string) error {
	regionNr, ok := rm.trainerRegion[trainerId]
	if !ok {
		return errors.New("user was not being tracked")
	}

	if region, ok := rm.activeRegions[regionNr]; ok {
		region.nrTrainers--
		if region.nrTrainers == 0 {
			log.Warnf("Disabling region %d", regionNr)
			delete(rm.activeRegions, regionNr)
		}
	}

	delete(rm.trainerRegion, trainerId)
	return nil
}

func (rm *RegionManager) GetRegionBoundsFromRegionNr(regionNr int) (topLeft utils.Location, botRight utils.Location) {

	topLeft = utils.Location{
		Longitude: float64((regionNr%rm.numRegionsPerAxis)*rm.regionSideLength) - 180,
		Latitude:  180 - float64(regionNr/rm.numRegionsPerAxis*rm.regionSideLength),
	}

	botRight = utils.Location{
		Latitude:  topLeft.Latitude - float64(rm.regionSideLength),
		Longitude: topLeft.Longitude + float64(rm.regionSideLength),
	}

	return topLeft, botRight
}

func (rm *RegionManager) GetTrainerRegion(trainerId string) (int, bool) {
	regionNr, ok := rm.trainerRegion[trainerId]
	return regionNr, ok
}

func (rm *RegionManager) getPokemonsInRegion(regionNr int) ([]utils.WildPokemon, error) {
	region, ok := rm.activeRegions[regionNr]
	if !ok {
		return nil, errors.New("region is nil")
	}
	pokemonsLock := region.pokemonLock
	defer pokemonsLock.RUnlock()
	pokemonsLock.RLock()
	pokemonsInRegion := region.pokemons
	/*


		var pokemonsInVicinity []utils.WildPokemon

		for _, pokemon := range pokemonsInRegion {
			distance := gps.CalcDistanceBetweenLocations(location, pokemon.Location)
			if distance <= config.Vicinity {
				pokemonsInVicinity = append(pokemonsInVicinity, pokemon)
			}
		}
	*/
	return pokemonsInRegion, nil
}

func (rm *RegionManager) getGymsInRegion(regionNr int) []utils.Gym {
	gymsLock.RLock()
	gymsInRegion, ok := rm.gymsFromRegion[regionNr]
	gymsLock.RUnlock()

	if !ok {
		return []utils.Gym{}
	}
	/*
		var gymsInVicinity []utils.Gym
			for _, gym := range gymsInRegion {
				distance := gps.CalcDistanceBetweenLocations(location, gym.Location)
				if distance <= config.Vicinity {
					gymsInVicinity = append(gymsInVicinity, gym)
				}
			}*/

	return gymsInRegion
}

func (rm *RegionManager) generateWildPokemonsForZonePeriodically(zoneNr int) {
	for region, ok := rm.activeRegions[zoneNr]; ok && region.nrTrainers != 0; {
		log.Info("Refreshing wild pokemons...")
		pokemonLock := region.pokemonLock
		pokemonLock.Lock()
		nrToGenerate := rm.maxPokemonsPerRegion - len(region.pokemons)
		if nrToGenerate > rm.maxPokemonsPerGeneration {
			nrToGenerate = rm.maxPokemonsPerGeneration
		}
		wildPokemons := generateWildPokemons(nrToGenerate, pokemonSpecies, region.TopLeftCorner, region.BotRightCorner)
		region.pokemons = append(region.pokemons, wildPokemons...)
		pokemonLock.Unlock()
		log.Infof("Added %d pokemons to zone %d", len(wildPokemons), zoneNr)
		time.Sleep(time.Duration(config.IntervalBetweenGenerations) * time.Second)
	}
	log.Warnf("Stopped generating pokemons for zone %d", zoneNr)
}

func (rm *RegionManager) cleanWildPokemons(regionNr int) {
	region, ok := rm.activeRegions[regionNr]
	if ok {
		region.pokemons = make([]utils.WildPokemon, 0)
	}
}

func (rm *RegionManager) logRegionManagerState() {
	log.Infof("Number of active regions: %d", len(rm.activeRegions))
	log.Infof("Number of active users: %d", len(rm.trainerRegion))
	for regionNr, region := range rm.activeRegions {
		log.Infof("---------------------Region %d---------------------", regionNr)
		log.Infof("Region bounds TopLeft:%+v, TopRight:%+v", region.TopLeftCorner, region.BotRightCorner)
		log.Info("Number of active users: ", region.nrTrainers)
		log.Info("Number of generated pokemons: ", len(region.pokemons))
		log.Info("Number of gyms: ", len(rm.gymsFromRegion[regionNr]))

		//for i, pokemon := range region.pokemons {
		//	log.Infof("Wild pokemon %d location: %+v", i, pokemon.Location)
		//}

		for _, gym := range rm.gymsFromRegion[regionNr] {
			log.Infof("Gym %s location: %+v", gym.Name, gym.Location)
		}

	}
}

func (rm *RegionManager) RemoveWildPokemonFromRegion(regionNr int, pokemonId string) (*pokemons.Pokemon, error) {
	region, ok := rm.activeRegions[regionNr]
	if !ok {
		return nil, errors.New("region is nil")
	}
	pokemonLock := region.pokemonLock
	pokemonLock.Lock()
	defer pokemonLock.Unlock()

	found := false
	var pokemon *pokemons.Pokemon
	for i, wp := range region.pokemons {
		if wp.Pokemon.Id.Hex() == pokemonId {
			found = true
			pokemon = &region.pokemons[i].Pokemon
			region.pokemons = append(region.pokemons[:i], region.pokemons[i+1:]...)
			break
		}
	}
	if !found {
		return nil, errors.New("pokemon not found")
	}
	return pokemon, nil
}

// auxiliary functions

func GetRegionNrFromLocation(location utils.Location, numRegionsPerAxis int, regionSideLength int) (int, error) {

	if location.Latitude >= LatitudeMax || location.Latitude <= -LatitudeMax {
		return -1, errors.New("latitude value out of bounds, bound is: -[85.05115 : 85.05115]")
	}
	if location.Longitude >= 179.9 || location.Longitude <= -179.9 {
		return -1, errors.New("latitude value out of bounds, bound is: -[179.9 : 179.9]")
	}

	latLong := s2.LatLngFromDegrees(location.Latitude, location.Longitude)

	proj := s2.NewMercatorProjection(180)
	transformedPoint := proj.FromLatLng(latLong)

	regionCol := int(math.Floor((180 + transformedPoint.X) / float64(regionSideLength)))
	regionRow := int(math.Floor((180 - transformedPoint.Y) / float64(regionSideLength)))
	regionNr := regionRow*numRegionsPerAxis + regionCol
	return regionNr, nil
}

func (rm *RegionManager) LoadGyms(gyms []utils.Gym) {

	rm.gymsFromRegion = make(map[int][]utils.Gym, len(gyms))

	for _, gym := range gyms {

		if isWithinBounds(gym.Location, rm.TopLeftCorner, rm.BotRightCorner) {
			log.Infof("Adding gym %s", gym.Name)
			if err := locationdb.AddGym(gym); err != nil {
				log.Error("Error adding gym")
			}
			regionNr, err := GetRegionNrFromLocation(gym.Location, rm.numRegionsPerAxis, rm.regionSideLength)
			if err != nil {
				log.Error(err)
				continue
			}
			rm.gymsFromRegion[regionNr] = append(rm.gymsFromRegion[regionNr], gym)
		} else {
			log.Infof("Gym %s out of bounds", gym.Name)
		}
	}

	for regionNr, gyms := range rm.gymsFromRegion {
		log.Infof("Region %d gyms: %+v", regionNr, gyms)
	}
}

func isWithinBounds(location utils.Location, topLeft utils.Location, botRight utils.Location) bool {
	if location.Longitude > botRight.Longitude || location.Longitude < topLeft.Longitude {
		return false
	}

	if location.Latitude < botRight.Latitude || location.Latitude > topLeft.Latitude {
		return false
	}
	return true
}
