package main

import (
	"fmt"
	"testing"

	"github.com/golang/geo/s2"
)

func TestCellManager_calculateLocationTileChanges(t *testing.T) {
	userLoc := s2.LatLngFromDegrees(0.5, 0.5)

	capLoc := calculateCapForLocation(userLoc, float64(500))

	trainersCellLevel := 18
	trainersRegionCoverer := s2.RegionCoverer{
		MinLevel: trainersCellLevel,
		MaxLevel: trainersCellLevel,
		LevelMod: 1,
		MaxCells: 1000,
	}

	fmt.Printf("cap: %+v\n", capLoc)

	newExitCellIds := trainersRegionCoverer.Covering(s2.Region(capLoc))

	for _, cellId := range newExitCellIds {
		fmt.Println("level: ", s2.CellFromCellID(cellId).Level())
	}
	fmt.Println(len(newExitCellIds))
}

func TestCellManager_testCapAndIntersection(t *testing.T) {

}
