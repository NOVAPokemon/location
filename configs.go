package main

import (
	"github.com/NOVAPokemon/utils"
)

type LocationServerConfig struct {
	Timeout               int `json:"timeout_interval"`
	Ping                  int `json:"ping_interval"`
	UpdateGymsInterval    int `json:"update_gyms_interval"`
	UpdatePokemonInterval int `json:"update_pokemon_interval"`
	UpdateConfigsInterval int `json:"update_config_interval"`

	// this is in meters
	Vicinity float64 `json:"vicinity"`

	// Generate configs
	IntervalBetweenGenerations int `json:"interval_generate"` // in minutes
	NumberOfPokemonsToGenerate int `json:"pokemons_to_generate"`

	MaxLevel  float64 `json:"max_level"`
	MaxHP     float64 `json:"max_hp"`
	MaxDamage float64 `json:"max_damage"`

	MaxPokemonsPerTile int            `json:"max_pokemons_per_tile"`
	NumTilesInWorld    int            `json:"num_tiles"`
	TopLeftCorner      utils.Location `json:"topLeftCorner"`
	BotRightCorner     utils.Location `json:"botRightCorner"`
}
