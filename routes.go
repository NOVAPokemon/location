package main

import (
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
)

const UserLocationName = "USER_LOCATION"
const GymLocationName = "GYM_LOCATION"
const CatchWildPokemonName = "CATCH_WILD_POKEMON"

const GET = "GET"
const POST = "POST"

var routes = utils.Routes{
	utils.Route{
		Name:        UserLocationName,
		Method:      GET,
		Pattern:     api.UserLocationRoute,
		HandlerFunc: HandleUserLocation,
	},

	utils.Route{
		Name:        GymLocationName,
		Method:      POST,
		Pattern:     api.GymLocationRoute,
		HandlerFunc: HandleAddGymLocation,
	},

	utils.Route{
		Name:        CatchWildPokemonName,
		Method:      GET,
		Pattern:     api.CatchWildPokemonRoute,
		HandlerFunc: HandleCatchWildPokemon,
	},
}
