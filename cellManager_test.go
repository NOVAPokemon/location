package main

import (
	"fmt"
	"testing"

	"github.com/golang/geo/s2"
)

func TestCellManager_calculateLocationTileChanges(t *testing.T) {
	userLoc := s2.LatLngFromDegrees(0.5, 0.5)

	capLoc := CalculateCapForLocation(userLoc, float64(500))

	trainersCellLevel := 15
	trainersRegionCoverer := s2.RegionCoverer{
		MinLevel: trainersCellLevel,
		MaxLevel: trainersCellLevel,
		LevelMod: 1,
	}

	newExitCellIds := trainersRegionCoverer.Covering(s2.Region(capLoc))

	fmt.Println(len(newExitCellIds))
}
