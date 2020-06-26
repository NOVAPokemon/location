package main

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	tileMetrics      = make(map[string]prometheus.Gauge)
	connectedClients = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "location_connected_clients",
		Help: "The total number of connected clients",
	})
)

func emitNrConnectedClients() {
	counter := 0
	clientChannels.Range(func(_, _ interface{}) bool {
		counter++
		return true
	})
	connectedClients.Set(float64(counter))
}

func emitNrConnectedTrainersInTile(tileNr int, numTrainers int32) {
	name := fmt.Sprintf("location_tile_%d_connected_clients", tileNr)
	gauge, ok := tileMetrics[name]
	if !ok {
		gauge = promauto.NewGauge(prometheus.GaugeOpts{
			Name: name,
			Help: "The total number of clients in this tile",
		})
		tileMetrics[name] = gauge
	}

	gauge.Set(float64(numTrainers))
}

func emitNrPokemonsInTile(tileNr int, tile *Tile) {
	name := fmt.Sprintf("location_tile_%d_wild_pokemons", tileNr)
	gauge, ok := tileMetrics[name]
	if !ok {
		gauge = promauto.NewGauge(prometheus.GaugeOpts{
			Name: name,
			Help: "The total number of pokemons in this tile",
		})
		tileMetrics[name] = gauge
	}

	numPokemons := 0
	tile.pokemons.Range(func(_, _ interface{}) bool {
		numPokemons++
		return true
	})

	gauge.Set(float64(numPokemons))
}

func emitTileMetrics() {
	tm.activeTiles.Range(func(key, value interface{}) bool {
		tileNr := key.(int)
		tile := value.(activeTileValueType)

		numTrainersValue, ok := tm.activeTileTrainerNumber.Load(tileNr)
		if !ok {
			return true
		}

		numTrainers := numTrainersValue.(activeTileTrainerNrValueType)

		emitNrConnectedTrainersInTile(tileNr, *numTrainers)
		emitNrPokemonsInTile(tileNr, tile)
		return true
	})
}

// metrics for prometheus
func recordMetrics() {
	go func() {
		for {
			emitNrConnectedClients()
			emitTileMetrics()
			time.Sleep(8 * time.Second)
		}
	}()
}
