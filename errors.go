package main

import (
	"fmt"
	"github.com/NOVAPokemon/utils"
	"github.com/pkg/errors"
)

const (
	errorHandleLocationMsg = "error handling location message"

	errorPokemonNotCatchableFormat = "pokemon %s is not available to catch"
)

var (
	errorInvalidItemCatch   = errors.New("invalid item to catch")
	errorLocationNotTracked = errors.New("location not being tracked")
)

// Wrappers handlers
func wrapAddGymError(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, AddGymLocationName))
}

func wrapUserLocationError(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, UserLocationName))
}

func wrapSetAreaError(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, SetRegionAreaName))
}

func wrapCatchWildPokemonError(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, CatchWildPokemonName))
}

// Wrappers other functions
func wrapHandleLocationMsgs(err error) error {
	return errors.Wrap(err, errorHandleLocationMsg)
}

// Error builders
func newPokemonNotAvailable(pokemonId string) error {
	return errors.New(fmt.Sprintf(errorPokemonNotCatchableFormat, pokemonId))
}
