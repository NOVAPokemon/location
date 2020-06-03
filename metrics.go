package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"time"
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
			connectedClients.Set(float64(len(clientChannels)))
			time.Sleep(2 * time.Second)
		}
	}()
}
