package main

import (
	"encoding/json"
	"fmt"
	"github.com/NOVAPokemon/utils"
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
	host = utils.ServeHost
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

	go generate(pokemonSpecies)

	time.Sleep(2 * time.Second)

	go updateGymsPeriodically()
	go updatePokemonPeriodically()

	log.Infof("Starting LOCATION server in port %d...\n", port)
	log.Fatal(http.ListenAndServe(addr, r))
}

func generate(pokemonSpecies []string) {
	for {
		log.Info("Refreshing wild pokemons...")
		cleanWildPokemons()
		generateWildPokemons(pokemonSpecies)
		time.Sleep(time.Duration(config.IntervalBetweenGenerations) * time.Minute)
	}
}

func generateWildPokemons(pokemonSpecies []string) {
	stdHPDeviation := config.MaxHP / 20
	stdDamageDeviation := config.MaxDamage / 20

	for i := 0; i < config.NumberOfPokemonsToGenerate; i++ {
		err, _ := locationdb.AddWildPokemon(*pokemons.GetOneWildPokemon(config.MaxLevel, stdHPDeviation,
			config.MaxHP, stdDamageDeviation, config.MaxDamage, pokemonSpecies[rand.Intn(len(pokemonSpecies))-1]))

		if err != nil {
			log.Error("Error adding wild pokemon")
			log.Error(err)
		}
	}
}

func cleanWildPokemons() {
	err := locationdb.DeleteWildPokemons()

	if err != nil {
		return
	}
}

func updateGymsPeriodically() {
	ticker := time.NewTicker(time.Duration(config.UpdateGymsInterval) * time.Second)
	defer ticker.Stop()

	if err := updateGyms(); err != nil {
		log.Error(err)
		return
	}

	for {
		gymsLock.Lock()
		if err := updateGyms(); err != nil {
			log.Error(err)
			return
		}
		gymsLock.Unlock()
		<-ticker.C
	}
}

func updateGyms() error {
	var err error
	gyms, err = locationdb.GetGyms()
	return err
}

func updatePokemonPeriodically() {
	ticker := time.NewTicker(time.Duration(config.UpdatePokemonInterval) * time.Second)
	defer ticker.Stop()

	if err := updatePokemon(); err != nil {
		log.Error(err)
		return
	}

	for {
		pokemonLock.Lock()
		if err := updatePokemon(); err != nil {
			log.Error(err)
			return
		}
		pokemonLock.Unlock()
		<-ticker.C
	}
}

func updatePokemon() error {
	var err error
	pokemon, err = locationdb.GetWildPokemons()
	return err
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
