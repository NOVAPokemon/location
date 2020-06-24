package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/NOVAPokemon/utils"
)

func TestMain(m *testing.M) {
	res := m.Run()
	os.Exit(res)
}

func TestTileManager_GetTileNrFromLocation(t *testing.T) {

	var (
		rm = NewTileManager(nil, 4, 100, 10,
			utils.Location{Latitude: LatitudeMax, Longitude: -180},
			utils.Location{Latitude: -LatitudeMax, Longitude: 180})
	)

	locationInTile0 := utils.Location{
		Latitude:  40,
		Longitude: -90,
	}

	tileNr, _, _, err := rm.GetTileNrFromLocation(locationInTile0)

	if err != nil {
		t.Error(err)
		t.Fail()
	}

	if tileNr != 0 {
		t.Error(fmt.Sprintf("Location %+v should be in tile 0, was: %d", locationInTile0, tileNr))
		t.Fail()
	}

	locationInTile1 := utils.Location{
		Latitude:  40,
		Longitude: 90,
	}

	tileNr, _, _, err = rm.GetTileNrFromLocation(locationInTile1)

	if err != nil {
		t.Error(err)
		t.Fail()
	}

	if tileNr != 1 {
		t.Error(fmt.Sprintf("Location %+v should be in tile 1, was: %d", locationInTile1, tileNr))
		t.Fail()
	}

	locationInTile2 := utils.Location{
		Latitude:  -40,
		Longitude: -90,
	}

	tileNr, _, _, err = rm.GetTileNrFromLocation(locationInTile2)

	if err != nil {
		t.Error(err)
		t.Fail()
	}

	if tileNr != 2 {
		t.Error(fmt.Sprintf("Location %+v should be in tile 2, was: %d", locationInTile2, tileNr))
		t.Fail()
	}

	locationInTile3 := utils.Location{
		Latitude:  -40,
		Longitude: 90,
	}

	tileNr, _, _, err = rm.GetTileNrFromLocation(locationInTile3)

	if err != nil {
		t.Error(err)
		t.Fail()
	}

	if tileNr != 3 {
		t.Error(fmt.Sprintf("Location %+v should be in tile 3, was: %d", locationInTile3, tileNr))
		t.Fail()
	}
}

func TestTileManager_GetTileBoundsFromTileNr(t *testing.T) {

	var (
		rm = NewTileManager(nil, 4, 100, 10,
			utils.Location{Latitude: LatitudeMax, Longitude: -180},
			utils.Location{Latitude: -LatitudeMax, Longitude: 180})
	)

	topLeft, botRight := tm.GetTileBoundsFromTileNr(0)

	if topLeft.Latitude != 180.0 {
		t.Error(fmt.Sprintf("tile 0 bot left Latitude is %f, should be %f", topLeft.Latitude, 180.0))
		t.Fail()
	}

	if topLeft.Longitude != -180.0 {
		t.Error(fmt.Sprintf("tile 0  bot left Longitude is %f, should be %f", topLeft.Longitude, -180.0))
		t.Fail()
	}

	if botRight.Latitude != 0.0 {
		t.Error(fmt.Sprintf("tile 0  Bot right Latitude is %f, should be %f", topLeft.Latitude, 0.0))
		t.Fail()
	}

	if botRight.Longitude != 0.0 {
		t.Error(fmt.Sprintf("tile 0 bot right Longitude is %f, should be %f", topLeft.Longitude, 0.0))
		t.Fail()
	}

	topLeft, botRight = rm.GetTileBoundsFromTileNr(1)

	if topLeft.Latitude != 180.0 {
		t.Error(fmt.Sprintf("tile 1 bot left Latitude is %f, should be %f", topLeft.Latitude, 180.0))
		t.Fail()
	}

	if topLeft.Longitude != 0.0 {
		t.Error(fmt.Sprintf("tile 1 bot left Longitude is %f, should be %f", topLeft.Longitude, 0.0))
		t.Fail()
	}

	if botRight.Latitude != 0.0 {
		t.Error(fmt.Sprintf("tile 1 Bot right Latitude is %f, should be %f", topLeft.Latitude, 0.0))
		t.Fail()
	}

	if botRight.Longitude != 180.0 {
		t.Error(fmt.Sprintf("tile 1 bot right Longitude is %f, should be %f", topLeft.Longitude, 180.0))
		t.Fail()
	}

	topLeft, botRight = rm.GetTileBoundsFromTileNr(2)

	if topLeft.Latitude != 0.0 {
		t.Error(fmt.Sprintf("tile 2 bot left Latitude is %f, should be %f", topLeft.Latitude, 0.0))
		t.Fail()
	}

	if topLeft.Longitude != -180.0 {
		t.Error(fmt.Sprintf("tile 2 bot left Longitude is %f, should be %f", topLeft.Longitude, -180.0))
		t.Fail()
	}

	if botRight.Latitude != -180.0 {
		t.Error(fmt.Sprintf("tile 2 Bot right Latitude is %f, should be %f", topLeft.Latitude, -180.0))
		t.Fail()
	}

	if botRight.Longitude != 0.0 {
		t.Error(fmt.Sprintf("tile 2 bot right Longitude is %f, should be %f", topLeft.Longitude, 0.0))
		t.Fail()
	}

	topLeft, botRight = rm.GetTileBoundsFromTileNr(3)

	if topLeft.Latitude != 0.0 {
		t.Error(fmt.Sprintf("tile 3 bot left Latitude is %f, should be %f", topLeft.Latitude, 0.0))
		t.Fail()
	}

	if topLeft.Longitude != 0.0 {
		t.Error(fmt.Sprintf("tile 3 bot left Longitude is %f, should be %f", topLeft.Longitude, 0.0))
		t.Fail()
	}

	if botRight.Latitude != -180.0 {
		t.Error(fmt.Sprintf("tile 3 Bot right Latitude is %f, should be %f", topLeft.Latitude, -180.0))
		t.Fail()
	}

	if botRight.Longitude != 180.0 {
		t.Error(fmt.Sprintf("tile 3 bot right Longitude is %f, should be %f", topLeft.Longitude, 180.0))
		t.Fail()
	}

}
