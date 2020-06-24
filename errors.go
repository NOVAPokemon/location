package main

import (
	"fmt"

	"github.com/NOVAPokemon/utils"
	"github.com/pkg/errors"
)

const (
	errorHandleLocationMsg    = "error handling location message"
	errorHandleLocationWithTilesMsg    = "error handling location with tiles message"
	errorLoadServerBoundaries = "error loading server boundaries"
	errorCatchingPokemon      = "error catching pokemon"
	errorInit                 = "error in INIT"
)

var (
	errorInvalidItemCatch   = errors.New("invalid item to catch")
)

// Wrappers handlers
func wrapAddGymError(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, AddGymLocationName))
}

func wrapUserLocationError(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, UserLocationName))
}

func wrapCatchWildPokemonError(err error) error {
	return errors.Wrap(err, errorCatchingPokemon)
}

func wrapSetServerConfigsError(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, SetServerConfigsName))
}

func wrapGetAllConfigs(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, GetGlobalRegionConfigName))
}

func wrapGetServerForLocation(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, GetServerForLocationName))
}

// Wrappers other functions
func wrapHandleLocationMsgs(err error) error {
	return errors.Wrap(err, errorHandleLocationMsg)
}

func wrapHandleLocationWithTilesMsgs(err error) error {
	return errors.Wrap(err, errorHandleLocationWithTilesMsg)
}

func WrapInit(err error) error {
	return errors.Wrap(err, errorInit)
}

func WrapLoadServerBoundaries(err error) error {
	return errors.Wrap(err, errorLoadServerBoundaries)
}
