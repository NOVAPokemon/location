package main

import (
	"encoding/json"
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/pokemons"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"math"
	"math/rand"
	"time"
)

const (
	host = utils.ServeHost
	port = utils.LocationPort

	PokemonsFilename = "pokemons.json"
	configFilename   = "configs.json"

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
	utils.CheckLogFlag(serviceName)

	pokemonSpecies = loadPokemonSpecies()
	time.Sleep(2 * time.Second)

	utils.StartServer(serviceName, host, port, routes)
}

// Pokemons taken from https://raw.githubusercontent.com/sindresorhus/pokemon/master/data/en.json
func loadPokemonSpecies() []string {
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

	if !isPerfectSquare(config.NumTilesInWorld) {
		log.Panic("Number of regions is not a perfect square (i.e. 4, 9, 16...)")
	}

	return &config
}

func isPerfectSquare(nr int) bool {
	sqrt := math.Sqrt(float64(nr))
	return sqrt*sqrt == float64(nr)

}

func generateWildPokemons(toGenerate int, pokemonSpecies []string, topLeft utils.Location, botRight utils.Location) []utils.WildPokemon {
	stdHPDeviation := config.MaxHP / 20
	stdDamageDeviation := config.MaxDamage / 20
	regionSize := topLeft.Latitude - botRight.Latitude
	pokemonsArr := make([]utils.WildPokemon, 0, config.NumberOfPokemonsToGenerate)

	if len(pokemonSpecies) == 0 {
		log.Panic("array pokemonSpecies is empty")
	}

	for i := 0; i < toGenerate; i++ {
		pokemon := *pokemons.GetOneWildPokemon(config.MaxLevel, stdHPDeviation,
			config.MaxHP, stdDamageDeviation, config.MaxDamage, pokemonSpecies[rand.Intn(len(pokemonSpecies)-1)])
		wildPokemon := utils.WildPokemon{
			Pokemon: pokemon,
			Location: utils.Location{
				Latitude:  topLeft.Latitude - rand.Float64()*regionSize,
				Longitude: topLeft.Longitude + rand.Float64()*regionSize,
			},
		}
		pokemonsArr = append(pokemonsArr, wildPokemon)
	}

	return pokemonsArr
}
