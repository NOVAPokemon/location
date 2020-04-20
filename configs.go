package main

type LocationServerConfig struct {
	Timeout            int `json:"timeout_interval"`
	Ping               int `json:"ping_interval"`
	UpdateGymsInterval int `json:"update_gyms_interval"`

	// this is in meters
	Vicinity float64 `json:"vicinity"`
}
