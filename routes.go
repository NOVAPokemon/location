package main

import (
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
)

const SubscribeLocationName = "SUBSCRIBE_LOCATION"

const GET = "GET"

var routes = utils.Routes{
	utils.Route{
		Name:        SubscribeLocationName,
		Method:      GET,
		Pattern:     api.SubscribeLocationRoute,
		HandlerFunc: handleSubscribeLocation,
	},
}
