package main

import (
	"fmt"
	"testing"

	"github.com/NOVAPokemon/utils"
	"github.com/golang/geo/s1"
	"github.com/golang/geo/s2"
)

func Test_getServersForCells(t *testing.T) {
	userLocation := s2.LatLng{
		Lat: s1.Angle(0.008727),
		Lng: s1.Angle(0.008727),
	}

	cell := s2.CellIDFromLatLng(userLocation)
	cells := []s2.CellID{cell}

	fmt.Println(cell)

	configs := map[string]utils.LocationServerCells{
		"location-0": {
			CellIdsStrings: []string{
				"1152921504606846976",
				"10376293541461622784",
			},
			ServerName: "location-0",
		},
		"location-1": {
			CellIdsStrings: []string{
				"3458764513820540928",
				"12682136550675316736",
			},
			ServerName: "location-1",
		},
		"location-2": {
			CellIdsStrings: []string{
				"5764607523034234880",
			},
			ServerName: "location-2",
		},
		"location-3": {
			CellIdsStrings: []string{
				"8070450532247928832",
			},
			ServerName: "location-3",
		},
	}

	servers := map[string]s2.CellUnion{}
	for serverName, config := range configs {
		cellIds := convertStringsToCellIds(config.CellIdsStrings)

		for _, cell := range cells {
			if cellIds.ContainsCellID(cell) {
				serverAddr := fmt.Sprintf("%s.%s", serverName, serviceNameHeadless)
				servers[serverAddr] = append(servers[serverAddr], cell)
			}
		}
	}

	fmt.Printf("%+v\n", servers)
}
