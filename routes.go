package main

import (
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
)

const UserLocationName = "USER_LOCATION"
const AddGymLocationName = "GYM_LOCATION"
const CatchWildPokemonName = "CATCH_WILD_POKEMON"

const GetGlobalRegionConfigName = "GET_GLOBAL_SERVER_CONFIG"
const GetServerForLocationName = "GET_SERVER_FOR_LOCATION"
const SetServerConfigsName = "SET_SERVER_CONFIG"
const ForceLoadConfigName = "RELOAD_SERVER_CONFIG"

const GET = "GET"
const POST = "POST"
const PUT = "PUT"

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
		Name:        GetServerForLocationName,
		Method:      GET,
		Pattern:     api.GetServerForLocationRoute,
		HandlerFunc: HandleGetServerForLocation,
	},

	utils.Route{
		Name:        SetServerConfigsName,
		Method:      PUT,
		Pattern:     api.SetServerConfigRoute,
		HandlerFunc: HandleSetServerConfigs,
	},

	utils.Route{
		Name:        GetGlobalRegionConfigName,
		Method:      GET,
		Pattern:     api.GetAllConfigsRoute,
		HandlerFunc: HandleGetGlobalRegionSettings,
	},

	utils.Route{
		Name:        ForceLoadConfigName,
		Method:      GET,
		Pattern:     api.ForceLoadConfigRoute,
		HandlerFunc: HandleForceLoadConfig,
	},
}
