package main

import "github.com/NOVAPokemon/utils"

type LocationServerConfig struct {
	Timeout               int `json:"timeout_interval"`
	Ping                  int `json:"ping_interval"`
	UpdateGymsInterval    int `json:"update_gyms_interval"`
	UpdatePokemonInterval int `json:"update_pokemon_interval"`
	MaxPokemonsPerRegion int `json:"max_pokemons_per_region"`

	// this is in meters
	Vicinity float64 `json:"vicinity"`

	// Generate configs
	IntervalBetweenGenerations int `json:"interval_generate"` //in minutes
	NumberOfPokemonsToGenerate int `json:"pokemons_to_generate"`

	MaxLevel  float64 `json:"max_level"`
	MaxHP     float64 `json:"max_hp"`
	MaxDamage float64 `json:"max_damage"`

	NumRegionsInWorld int            `json:"num_regions"`
	TopLeftCorner     utils.Location `json:"topLeftCorner"`
	BotRightCorner    utils.Location `json:"botRightCorner"`
}
