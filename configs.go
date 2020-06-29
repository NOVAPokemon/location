package main

import (
	"github.com/golang/geo/s2"
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
	PokemonCellLevel           int `json:"pokemon_cell_level"`

	GymsCellLevel int `json:"gyms_cell_level"`

	MaxLevel  float64 `json:"max_level"`
	MaxHP     float64 `json:"max_hp"`
	MaxDamage float64 `json:"max_damage"`

	Cells             s2.CellUnion `json:"Cells"`
	TrainersCellLevel int          `json:"trainers_cell_level"`

	EntryBoundaryLevel int `json:"entry_boundary_size"`
	ExitBoundaryLevel  int `json:"exit_boundary_size"`
}
