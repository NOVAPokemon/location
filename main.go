package main

import (
	"encoding/json"
	"fmt"
	"github.com/NOVAPokemon/utils"
	generatordb "github.com/NOVAPokemon/utils/database/generator"
	locationdb "github.com/NOVAPokemon/utils/database/location"
	"github.com/NOVAPokemon/utils/pokemons"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"math/rand"
	"net/http"
	"time"
)

const (
	host = utils.Host
	port = utils.LocationPort

	PokemonsFilename = "pokemons.json"
	configFilename   = "configs.json"
)

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
	config = loadConfig()
)

func main() {
	addr := fmt.Sprintf("%s:%d", host, port)
	pokemonSpecies := loadPokemons()

	rand.Seed(time.Now().Unix())

	r := utils.NewRouter(routes)

	loadExampleGyms()
	go updateGymsPeriodically()
	go generate(pokemonSpecies)

	log.Infof("Starting LOCATION server in port %d...\n", port)
	log.Fatal(http.ListenAndServe(addr, r))
}

func generate(pokemonSpecies []string) {
	for {
		log.Info("Refreshing wild pokemons...")
		cleanWildPokemons()
		generateWildPokemons(pokemonSpecies)
		log.Info("Refreshing catchable items...")
		time.Sleep(time.Duration(config.IntervalBetweenGenerations) * time.Minute)
	}
}

func generateWildPokemons(pokemonSpecies []string) {
	stdHPDeviation := config.MaxHP / 20
	stdDamageDeviation := config.MaxDamage / 20

	for i := 0; i < config.NumberOfPokemonsToGenerate; i++ {
		err, _ := generatordb.AddWildPokemon(*pokemons.GetOneWildPokemon(config.MaxLevel, stdHPDeviation,
			config.MaxHP, stdDamageDeviation, config.MaxDamage, pokemonSpecies[rand.Intn(len(pokemonSpecies))-1]))

		if err != nil {
			log.Error("Error adding wild pokemon")
			log.Error(err)
		}
	}
}

func cleanWildPokemons() {
	err := generatordb.DeleteWildPokemons()

	if err != nil {
		return
	}
}

func updateGymsPeriodically() {
	lock.Lock()
	defer lock.Unlock()

	timer := time.NewTimer(time.Duration(config.UpdateGymsInterval) * time.Second)

	for {
		var err error
		gyms, err = locationdb.GetGyms()
		if err != nil {
			return
		}

		<-timer.C
		timer.Reset(time.Duration(config.UpdateGymsInterval) * time.Second)
	}
}

// Pokemons taken from https://raw.githubusercontent.com/sindresorhus/pokemon/master/data/en.json
func loadPokemons() []string {
	data, err := ioutil.ReadFile(PokemonsFilename)
	if err != nil {
		log.Fatal("Error loading pokemons file")
		return nil
	}

	var pokemonNames []string
	err = json.Unmarshal(data, &pokemonNames)

	if err != nil {
		log.Errorf("Error unmarshalling pokemons name")
		log.Fatal(err)
	}

	log.Infof("Loaded %d pokemon species.", len(pokemonNames))

	return pokemonNames
}

func loadConfig() *LocationServerConfig {
	fileData, err := ioutil.ReadFile(configFilename)
	if err != nil {
		log.Error(err)
		return nil
	}

	var config LocationServerConfig
	err = json.Unmarshal(fileData, &config)
	if err != nil {
		log.Error(err)
		return nil
	}

	return &config
}

func loadExampleGyms() {
	if err := locationdb.DeleteAllGyms(); err != nil {
		log.Error(err)
		return
	}

	fileData, err := ioutil.ReadFile(exampleGymsFilename)
	if err != nil {
		log.Error(err)
		return
	}

	var gyms []utils.Gym
	err = json.Unmarshal(fileData, &gyms)
	if err != nil {
		log.Error(err)
		return
	}

	for _, gym := range gyms {
		if err := locationdb.AddGym(gym); err != nil {
			log.Error(err)
		}
	}
}
