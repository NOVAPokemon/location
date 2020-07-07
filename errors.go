package main

import (
	"fmt"

	"github.com/NOVAPokemon/utils"
	"github.com/pkg/errors"
)

const (
	errorHandleLocationMsg          = "error handling location message"
	errorHandleLocationWithTilesMsg = "error handling location with tiles message"
	errorLoadServerBoundaries       = "error loading server boundaries"
	errorCatchingPokemon            = "error catching pokemon"
	errorInit                       = "error in INIT"
	errorAddingGym                  = "error adding gym %s"
	errorPokemonNotFoundFormat      = "could not find pokemon %s"
)

var (
	errorInvalidItemCatch = errors.New("invalid item to catch")
)

// Wrappers handlers

func wrapSetGymsError(err error, gymId string) error {
	return errors.Wrap(err, fmt.Sprintf(errorAddingGym, gymId))
}

func wrapAddGymError(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, addGymLocationName))
}

func wrapUserLocationError(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, userLocationName))
}

func wrapCatchWildPokemonError(err error) error {
	return errors.Wrap(err, errorCatchingPokemon)
}

func wrapSetServerConfigsError(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, setServerConfigsName))
}

func wrapGetAllConfigs(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, getGlobalRegionConfigName))
}

func wrapGetServerForLocation(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, getServerForLocationName))
}

// Wrappers other functions
func wrapHandleLocationMsgs(err error) error {
	return errors.Wrap(err, errorHandleLocationMsg)
}

func wrapHandleLocationWithTilesMsgs(err error) error {
	return errors.Wrap(err, errorHandleLocationWithTilesMsg)
}

func wrapInit(err error) error {
	return errors.Wrap(err, errorInit)
}

func wrapLoadServerBoundaries(err error) error {
	return errors.Wrap(err, errorLoadServerBoundaries)
}

func newPokemonNotFoundError(pokemonId string) error {
	return errors.New(fmt.Sprintf(errorPokemonNotFoundFormat, pokemonId))
}
