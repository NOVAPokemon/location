package main

import (
	"github.com/NOVAPokemon/utils"
	"github.com/golang/geo/s2"
	"sync"
	"sync/atomic"
)

type pokemonsInCellValueType = *utils.WildPokemonWithServer

type ActiveCell struct {
	pokemonCellsLevel int
	cellID            s2.CellID
	nrTrainers        *int64
	wildPokemons      *sync.Map
	cellMutex         *sync.RWMutex
}

func NewActiveCell(id s2.CellID, pokemonCellsLevel int) *ActiveCell {
	var nrTrainers int64 = 0
	return &ActiveCell{
		nrTrainers:        &nrTrainers,
		wildPokemons:      &sync.Map{},
		cellID:            id,
		cellMutex:         &sync.RWMutex{},
		pokemonCellsLevel: pokemonCellsLevel,
	}
}

func (ac *ActiveCell) GetPokemonsInCell() []utils.WildPokemonWithServer {
	ac.cellMutex.RLock()
	var wildPokemons []utils.WildPokemonWithServer
	ac.wildPokemons.Range(func(_, pokemon interface{}) bool {
		wildPokemons = append(wildPokemons, *pokemon.(pokemonsInCellValueType))
		return true
	})
	ac.cellMutex.RUnlock()
	return wildPokemons
}

func (ac *ActiveCell) RemovePokemon(wildPokemon utils.WildPokemonWithServer) (ok bool) {
	ac.cellMutex.RLock()
	pokemonCell := s2.CellIDFromLatLng(wildPokemon.Location)
	if _, ok := ac.wildPokemons.Load(pokemonCell); ok {
		ac.wildPokemons.Delete(pokemonCell)
	}
	ac.cellMutex.RUnlock()
	return ok
}

func (ac *ActiveCell) ExistsPokemon(wildPokemon utils.WildPokemonWithServer) (ok bool) {
	ac.cellMutex.RLock()
	pokemonCell := s2.CellIDFromLatLng(wildPokemon.Location)
	_, ok = ac.wildPokemons.Load(pokemonCell)
	ac.cellMutex.RUnlock()
	return ok
}

func (ac *ActiveCell) GetPokemon(wildPokemon utils.WildPokemonWithServer) (*utils.WildPokemonWithServer, bool) {
	ac.cellMutex.RLock()
	pokemonCell := s2.CellIDFromLatLng(wildPokemon.Location)
	pokemon, ok := ac.wildPokemons.Load(pokemonCell)
	ac.cellMutex.RUnlock()
	if ok {
		toReturn := pokemon.(pokemonsInCellValueType)
		return toReturn, ok
	} else {
		return nil, ok
	}
}

func (ac *ActiveCell) AddPokemons(wildPokemons []utils.WildPokemonWithServer) {
	ac.cellMutex.RLock()
	for _, wildPokemon := range wildPokemons {
		pokemonCell := s2.CellIDFromLatLng(wildPokemon.Location)
		ac.wildPokemons.LoadOrStore(pokemonCell, wildPokemon)
	}
	ac.cellMutex.RUnlock()
}

// TODO is this lock necessary
func (ac *ActiveCell) RemoveTrainer() int64 {
	return atomic.AddInt64(ac.nrTrainers, -1)
}

// TODO is this lock necessary
func (ac *ActiveCell) AddTrainer() int64 {
	return atomic.AddInt64(ac.nrTrainers, 1)
}

// TODO is this lock necessary
func (ac *ActiveCell) GetNrTrainers() int64 {
	return atomic.LoadInt64(ac.nrTrainers)
}

func (ac *ActiveCell) AcquireReadLock() {
	ac.cellMutex.RLock()
}

func (ac *ActiveCell) ReleaseReadLock() {
	ac.cellMutex.RUnlock()
}

func (ac *ActiveCell) AcquireWriteLock() {
	ac.cellMutex.Lock()
}

func (ac *ActiveCell) ReleaseWriteLock() {
	ac.cellMutex.Unlock()
}
