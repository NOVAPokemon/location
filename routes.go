package main

import (
	"strings"

	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
)

const (
	userLocationName   = "USER_LOCATION"
	addGymLocationName = "GYM_LOCATION"

	getGlobalRegionConfigName = "GET_GLOBAL_SERVER_CONFIG"
	getServerForLocationName  = "GET_SERVER_FOR_LOCATION"
	setServerConfigsName      = "SET_SERVER_CONFIG"
	forceLoadConfigName       = "RELOAD_SERVER_CONFIG"
	getActiveCells            = "GET_ACTIVE_CELLS"
	getActivePokemons         = "GET_ACTIVE_POKEMONS"
)

const (
	get  = "GET"
	post = "POST"
	put  = "PUT"
)

var routes = utils.Routes{
	api.GenStatusRoute(strings.ToLower(serviceName)),
	utils.Route{
		Name:        userLocationName,
		Method:      get,
		Pattern:     api.UserLocationRoute,
		HandlerFunc: handleUserLocation,
	},

	utils.Route{
		Name:        addGymLocationName,
		Method:      post,
		Pattern:     api.GymLocationRoute,
		HandlerFunc: handleAddGymLocation,
	},

	utils.Route{
		Name:        getServerForLocationName,
		Method:      get,
		Pattern:     api.GetServerForLocationRoute,
		HandlerFunc: handleGetServerForLocation,
	},

	utils.Route{
		Name:        setServerConfigsName,
		Method:      put,
		Pattern:     api.SetServerConfigRoute,
		HandlerFunc: handleSetServerConfigs,
	},

	utils.Route{
		Name:        getGlobalRegionConfigName,
		Method:      get,
		Pattern:     api.GetAllConfigsRoute,
		HandlerFunc: handleGetGlobalRegionSettings,
	},

	utils.Route{
		Name:        forceLoadConfigName,
		Method:      get,
		Pattern:     api.ForceLoadConfigRoute,
		HandlerFunc: handleForceLoadConfig,
	},

	utils.Route{
		Name:        getActiveCells,
		Method:      get,
		Pattern:     api.GetActiveCellsRoute,
		HandlerFunc: handleGetActiveCells,
	},

	utils.Route{
		Name:        getActivePokemons,
		Method:      get,
		Pattern:     api.GetActivePokemonsRoute,
		HandlerFunc: handleGetActivePokemons,
	},
}
