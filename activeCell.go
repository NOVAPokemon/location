package main

import (
	"sync"
	"sync/atomic"

	"github.com/NOVAPokemon/utils"
	"github.com/golang/geo/s2"
)

type pokemonsInCellValueType = *utils.WildPokemonWithServer

type activeCell struct {
	pokemonCellsLevel int
	cellID            s2.CellID
	nrTrainers        *int64
	wildPokemons      *sync.Map
	cellMutex         *sync.RWMutex
}

func newActiveCell(id s2.CellID, pokemonCellsLevel int) *activeCell {
	var nrTrainers int64 = 0
	return &activeCell{
		nrTrainers:        &nrTrainers,
		wildPokemons:      &sync.Map{},
		cellID:            id,
		cellMutex:         &sync.RWMutex{},
		pokemonCellsLevel: pokemonCellsLevel,
	}
}

func (ac *activeCell) getPokemonsInCell() []utils.WildPokemonWithServer {
	var wildPokemons []utils.WildPokemonWithServer
	ac.wildPokemons.Range(func(_, pokemon interface{}) bool {
		wildPokemons = append(wildPokemons, *pokemon.(pokemonsInCellValueType))
		return true
	})
	return wildPokemons
}

func (ac *activeCell) removePokemon(wildPokemon utils.WildPokemonWithServer) (ok bool) {
	pokemonCell := s2.CellIDFromLatLng(wildPokemon.Location)
	if _, ok = ac.wildPokemons.Load(pokemonCell); ok {
		ac.wildPokemons.Delete(pokemonCell)
	}
	return ok
}

func (ac *activeCell) existsPokemon(wildPokemon utils.WildPokemonWithServer) (ok bool) {
	pokemonCell := s2.CellIDFromLatLng(wildPokemon.Location)
	_, ok = ac.wildPokemons.Load(pokemonCell)
	return ok
}

func (ac *activeCell) getPokemon(wildPokemon utils.WildPokemonWithServer) (*utils.WildPokemonWithServer, bool) {
	pokemonCell := s2.CellIDFromLatLng(wildPokemon.Location)
	pokemon, ok := ac.wildPokemons.Load(pokemonCell)
	if ok {
		toReturn := pokemon.(pokemonsInCellValueType)
		return toReturn, ok
	} else {
		return nil, ok
	}
}

func (ac *activeCell) addPokemons(wildPokemons []utils.WildPokemonWithServer) {
	for _, wildPokemon := range wildPokemons {
		pokemonCell := s2.CellIDFromLatLng(wildPokemon.Location)
		ac.wildPokemons.LoadOrStore(pokemonCell, wildPokemon)
	}
}

func (ac *activeCell) removeTrainer() int64 {
	return atomic.AddInt64(ac.nrTrainers, -1)
}

func (ac *activeCell) addTrainer() int64 {
	val := atomic.AddInt64(ac.nrTrainers, 1)
	return val
}

func (ac *activeCell) getNrTrainers() int64 {
	return atomic.LoadInt64(ac.nrTrainers)
}

func (ac *activeCell) acquireReadLock() {
	ac.cellMutex.RLock()
}

func (ac *activeCell) releaseReadLock() {
	ac.cellMutex.RUnlock()
}

func (ac *activeCell) acquireWriteLock() {
	ac.cellMutex.Lock()
}

func (ac *activeCell) releaseWriteLock() {
	ac.cellMutex.Unlock()
}
