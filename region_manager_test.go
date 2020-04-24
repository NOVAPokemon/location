package main

import (
	"fmt"
	"github.com/NOVAPokemon/utils"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	res := m.Run()
	os.Exit(res)
}

func TestRegionManager_GetRegionNrFromLocation(t *testing.T) {

	var (
		rm = NewRegionManager(nil, 4, 100, 10,
			utils.Location{Latitude: LatitudeMax, Longitude: -180},
			utils.Location{Latitude: -LatitudeMax, Longitude: 180})
	)

	locationInRegion0 := utils.Location{
		Latitude:  40,
		Longitude: -90,
	}

	regionNr, err := GetRegionNrFromLocation(locationInRegion0, rm.numRegionsPerAxis, rm.regionSideLength)

	if err != nil {
		t.Error(err)
		t.Fail()
	}

	if regionNr != 0 {
		t.Error(fmt.Sprintf("Location %+v should be in region 0, was: %d", locationInRegion0, regionNr))
		t.Fail()
	}

	locationInRegion1 := utils.Location{
		Latitude:  40,
		Longitude: 90,
	}

	regionNr, err = GetRegionNrFromLocation(locationInRegion1, rm.numRegionsPerAxis, rm.regionSideLength)

	if err != nil {
		t.Error(err)
		t.Fail()
	}

	if regionNr != 1 {
		t.Error(fmt.Sprintf("Location %+v should be in region 1, was: %d", locationInRegion1, regionNr))
		t.Fail()
	}

	locationInRegion2 := utils.Location{
		Latitude:  -40,
		Longitude: -90,
	}

	regionNr, err = GetRegionNrFromLocation(locationInRegion2, rm.numRegionsPerAxis, rm.regionSideLength)

	if err != nil {
		t.Error(err)
		t.Fail()
	}

	if regionNr != 2 {
		t.Error(fmt.Sprintf("Location %+v should be in region 2, was: %d", locationInRegion2, regionNr))
		t.Fail()
	}

	locationInRegion3 := utils.Location{
		Latitude:  -40,
		Longitude: 90,
	}

	regionNr, err = GetRegionNrFromLocation(locationInRegion3, rm.numRegionsPerAxis, rm.regionSideLength)

	if err != nil {
		t.Error(err)
		t.Fail()
	}

	if regionNr != 3 {
		t.Error(fmt.Sprintf("Location %+v should be in region 3, was: %d", locationInRegion3, regionNr))
		t.Fail()
	}
}

func TestRegionManager_GetRegionBoundsFromRegionNr(t *testing.T) {

	var (
		rm = NewRegionManager(nil, 4, 100, 10,
			utils.Location{Latitude: LatitudeMax, Longitude: -180},
			utils.Location{Latitude: -LatitudeMax, Longitude: 180})
	)

	topLeft, botRight := rm.GetRegionBoundsFromRegionNr(0)

	if topLeft.Latitude != 180.0 {
		t.Error(fmt.Sprintf("region 0 bot left Latitude is %f, should be %f", topLeft.Latitude, 180.0))
		t.Fail()
	}

	if topLeft.Longitude != -180.0 {
		t.Error(fmt.Sprintf("region 0  bot left Longitude is %f, should be %f", topLeft.Longitude, -180.0))
		t.Fail()
	}

	if botRight.Latitude != 0.0 {
		t.Error(fmt.Sprintf("region 0  Bot right Latitude is %f, should be %f", topLeft.Latitude, 0.0))
		t.Fail()
	}

	if botRight.Longitude != 0.0 {
		t.Error(fmt.Sprintf("region 0 bot right Longitude is %f, should be %f", topLeft.Longitude, 0.0))
		t.Fail()
	}

	topLeft, botRight = rm.GetRegionBoundsFromRegionNr(1)

	if topLeft.Latitude != 180.0 {
		t.Error(fmt.Sprintf("region 1 bot left Latitude is %f, should be %f", topLeft.Latitude, 180.0))
		t.Fail()
	}

	if topLeft.Longitude != 0.0 {
		t.Error(fmt.Sprintf("region 1 bot left Longitude is %f, should be %f", topLeft.Longitude, 0.0))
		t.Fail()
	}

	if botRight.Latitude != 0.0 {
		t.Error(fmt.Sprintf("region 1 Bot right Latitude is %f, should be %f", topLeft.Latitude, 0.0))
		t.Fail()
	}

	if botRight.Longitude != 180.0 {
		t.Error(fmt.Sprintf("region 1 bot right Longitude is %f, should be %f", topLeft.Longitude, 180.0))
		t.Fail()
	}

	topLeft, botRight = rm.GetRegionBoundsFromRegionNr(2)

	if topLeft.Latitude != 0.0 {
		t.Error(fmt.Sprintf("region 2 bot left Latitude is %f, should be %f", topLeft.Latitude, 0.0))
		t.Fail()
	}

	if topLeft.Longitude != -180.0 {
		t.Error(fmt.Sprintf("region 2 bot left Longitude is %f, should be %f", topLeft.Longitude, -180.0))
		t.Fail()
	}

	if botRight.Latitude != -180.0 {
		t.Error(fmt.Sprintf("region 2 Bot right Latitude is %f, should be %f", topLeft.Latitude, -180.0))
		t.Fail()
	}

	if botRight.Longitude != 0.0 {
		t.Error(fmt.Sprintf("region 2 bot right Longitude is %f, should be %f", topLeft.Longitude, 0.0))
		t.Fail()
	}

	topLeft, botRight = rm.GetRegionBoundsFromRegionNr(3)

	if topLeft.Latitude != 0.0 {
		t.Error(fmt.Sprintf("region 3 bot left Latitude is %f, should be %f", topLeft.Latitude, 0.0))
		t.Fail()
	}

	if topLeft.Longitude != 0.0 {
		t.Error(fmt.Sprintf("region 3 bot left Longitude is %f, should be %f", topLeft.Longitude, 0.0))
		t.Fail()
	}

	if botRight.Latitude != -180.0 {
		t.Error(fmt.Sprintf("region 3 Bot right Latitude is %f, should be %f", topLeft.Latitude, -180.0))
		t.Fail()
	}

	if botRight.Longitude != 180.0 {
		t.Error(fmt.Sprintf("region 3 bot right Longitude is %f, should be %f", topLeft.Longitude, 180.0))
		t.Fail()
	}

}
