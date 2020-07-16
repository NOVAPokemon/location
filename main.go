package main

import (
	"encoding/json"
	"io/ioutil"
	"math/rand"

	"github.com/golang/geo/s2"

	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/pokemons"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

const (
	host = utils.ServeHost
	port = utils.LocationPort

	pokemonsFilename            = "wildPokemons.json"
	configFilename              = "configs.json"
	defaultServerBoundariesFile = "default_server_locations.json"

	serviceName = "LOCATION"
)

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
	config         = loadConfig()
	pokemonSpecies []string
)

func main() {
	flags := utils.ParseFlags(serverName)

	if !*flags.LogToStdout {
		utils.SetLogFile(serverName)
	}

	if utils.CheckDelayedFlag(*flags.DelayedComms) {
		commsManager = utils.CreateDefaultCommunicationManager()
	} else {
		locationTag := utils.GetLocationTag(utils.DefaultLocationTagsFilename, serverName)
		commsManager = utils.CreateDelayedCommunicationManager(utils.DefaultDelayConfigFilename, locationTag)
	}

	pokemonSpecies = loadPokemonSpecies()
	recordMetrics()
	utils.StartServer(serviceName, host, port, routes, commsManager)
}

// Pokemons taken from https://raw.githubusercontent.com/sindresorhus/pokemon/master/data/en.json
func loadPokemonSpecies() []string {
	data, err := ioutil.ReadFile(pokemonsFilename)
	if err != nil {
		log.Fatal("Error loading wildPokemons file")
		return nil
	}

	var pokemonNames []string
	err = json.Unmarshal(data, &pokemonNames)

	if err != nil {
		log.Errorf("Error unmarshalling wildPokemons name")
		log.Fatal(err)
	}

	log.Infof("Loaded %d pokemon species.", len(pokemonNames))

	return pokemonNames
}

func loadConfig() *locationServerConfig {
	fileData, err := ioutil.ReadFile(configFilename)
	if err != nil {
		log.Error(err)
		return nil
	}

	var configAux locationServerConfig
	err = json.Unmarshal(fileData, &configAux)
	if err != nil {
		log.Error(err)
		return nil
	}

	return &configAux
}

func generateWildPokemon(pokemonSpecies []string, cellId s2.CellID) utils.WildPokemonWithServer {
	stdHPDeviation := config.MaxHP / 20
	stdDamageDeviation := config.MaxDamage / 20
	pokemonPos := cellId.LatLng()

	if len(pokemonSpecies) == 0 {
		log.Panic("array pokemonSpecies is empty")
	}

	pokemon := *pokemons.GetOneWildPokemon(config.MaxLevel, stdHPDeviation,
		config.MaxHP, stdDamageDeviation, config.MaxDamage, pokemonSpecies[rand.Intn(len(pokemonSpecies)-1)])
	return utils.WildPokemonWithServer{
		Location: pokemonPos,
		Pokemon:  pokemon,
		Server:   serverName,
	}
}
