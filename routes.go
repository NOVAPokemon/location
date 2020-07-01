package main

import (
	"strings"

	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
)

const UserLocationName = "USER_LOCATION"
const AddGymLocationName = "GYM_LOCATION"

const GetGlobalRegionConfigName = "GET_GLOBAL_SERVER_CONFIG"
const GetServerForLocationName = "GET_SERVER_FOR_LOCATION"
const SetServerConfigsName = "SET_SERVER_CONFIG"
const ForceLoadConfigName = "RELOAD_SERVER_CONFIG"
const GetActiveCells = "GET_ACTIVE_CELLS"

const GET = "GET"
const POST = "POST"
const PUT = "PUT"

var routes = utils.Routes{
	api.GenStatusRoute(strings.ToLower(serviceName)),
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

	utils.Route{
		Name:        GetActiveCells,
		Method:      GET,
		Pattern:     api.GetActiveTiles,
		HandlerFunc: HandleGetActiveCells,
	},
}
