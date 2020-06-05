package main

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	connectedClients = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "location_connected_clients",
		Help: "The total number of connected clients",
	})
)

// metrics for prometheus
func recordMetrics() {
	go func() {
		for {
			counter := 0
			clientChannels.Range(func(_, _ interface{}) bool {
				counter++
				return true
			})

			connectedClients.Set(float64(counter))
			time.Sleep(8 * time.Second)
		}
	}()
}
