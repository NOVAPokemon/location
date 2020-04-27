package main

import (
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
)

const UserLocationName = "USER_LOCATION"
const AddGymLocationName = "GYM_LOCATION"
const CatchWildPokemonName = "CATCH_WILD_POKEMON"
const SetRegionAreaName = "SET_REGION"

const GET = "GET"
const POST = "POST"

var routes = utils.Routes{
	api.DefaultRoute,
	utils.Route{
		Name:        UserLocationName,
		Method:      GET,
		Pattern:     api.UserLocationRoute,
		HandlerFunc: HandleUserLocation,
	},

	utils.Route{
		Name:        AddGymLocationName,
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

	utils.Route{
		Name:    SetRegionAreaName,
		Method:  POST,
		Pattern: api.SetRegionAreaPath,
		HandlerFunc: HandleSetArea,
	},
}
