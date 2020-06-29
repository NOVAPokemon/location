package main

import (
	"fmt"
	"math"
	"os"
	"sync"
	"testing"

	"github.com/NOVAPokemon/utils"
	locationUtils "github.com/NOVAPokemon/utils/location"
	"github.com/golang/geo/s2"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	res := m.Run()
	os.Exit(res)
}

/*
func TestTileManager_GetTileNrFromLocation(t *testing.T) {

	var (
		rm = NewCellManager(nil, 4, 100, 10,
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
}*/

var (
	numTiles        = 64
	numTilesPerAxis = int(math.Sqrt(float64(numTiles)))
	tileSide        = 360.0 / float64(numTilesPerAxis)
	tm1             = &CellManager{
		numTilesInWorld:          numTiles,
		activeCells:              sync.Map{},
		trainerCells:             sync.Map{},
		numTilesPerAxis:          numTilesPerAxis,
		tileSideLength:           tileSide,
		maxPokemonsPerTile:       100,
		maxPokemonsPerGeneration: 10,
		proj:                     s2.NewMercatorProjection(180),
		activeCellsLock:          sync.Mutex{},
	}
)

func TestTileManager_CalculateBoundaryForLocation(t *testing.T) {
	lastRow := -1
	for i := 85.0; i > -85; i -= 1 {
		locationInCenter := utils.Location{
			Latitude:  i,
			Longitude: 0.5,
		}

		tileNr, row, col, err := tm1.GetTileNrFromLocation(locationInCenter, false)
		assert.Equal(t, 28, tileNr)
		assert.Equal(t, 3, row)
		assert.Equal(t, 4, col)
		rect := tm1.CalculateCapForLocation(row, col, 1)

		if err != nil {
			t.Fatal(err)
		}
		if lastRow != row {
			fmt.Printf("Doing: lat: %f, lon: %f\n", locationInCenter.Latitude, locationInCenter.Longitude)
			fmt.Println(row)
			fmt.Println(col)
			fmt.Println(tileNr)
			fmt.Println(rect.X)
			fmt.Println(rect.Y)
			lastRow = row
		}

		assert.Equal(t, 3.0, rect.X.Lo)
		assert.Equal(t, 2.0, rect.Y.Lo)

		assert.Equal(t, 5.0, rect.X.Hi)
		assert.Equal(t, 4.0, rect.Y.Hi)
	}

}

func TestTileManager_GetTileNrFromLocation(t *testing.T) {
	locationInCenter := utils.Location{
		Latitude:  0.5,
		Longitude: 0.5,
	}
	tileNr, row, col, err := tm1.GetTileNrFromLocation(locationInCenter, false)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(row)
	fmt.Println(col)
	fmt.Println(tileNr)

	assert.Equal(t, 28, tileNr)
	assert.Equal(t, 3, row)
	assert.Equal(t, 4, col)

}

func TestTileManager_getBoundaryTiles(t *testing.T) {
	locationInCenter := utils.Location{
		Latitude:  0.5,
		Longitude: 0.5,
	}
	tileNr, row, col, err := tm1.GetTileNrFromLocation(locationInCenter, false)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, 28, tileNr)
	assert.Equal(t, 3, row)
	assert.Equal(t, 4, col)

	boundaryTiles := tm1.getBoundaryTiles(row, col, 1)

	fmt.Println(boundaryTiles)

	assert.Contains(t, boundaryTiles, 19)
	assert.Contains(t, boundaryTiles, 20)
	assert.Contains(t, boundaryTiles, 21)
	assert.Contains(t, boundaryTiles, 27)
	assert.Contains(t, boundaryTiles, 28)
	assert.Contains(t, boundaryTiles, 29)
	assert.Contains(t, boundaryTiles, 35)
	assert.Contains(t, boundaryTiles, 36)
	assert.Contains(t, boundaryTiles, 37)
	assert.Equal(t, len(boundaryTiles), 9)

}

func TestTileManager_CalculateTileChanges(t *testing.T) {
	locationInCenter := utils.Location{
		Latitude:  0.5,
		Longitude: 0.5,
	}

	tileNr, row, col, err := tm1.GetTileNrFromLocation(locationInCenter, false)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, 44, tileNr)
	assert.Equal(t, 5, row)
	assert.Equal(t, 4, col)

	trainerId := "teste"
	tm1.trainerCells.Store(trainerId, []int{0, 1, 2, 8, 9, 10, 16, 17, 18})
	tm1.exitBoundarySize = 2
	tm1.entryBoundarySize = 1
	toRemove, toAdd, curr := tm1.calculateLocationTileChanges(trainerId, row, col)

	fmt.Println(toRemove)
	fmt.Println(toAdd)
	fmt.Println(curr)

	assert.Contains(t, toRemove, 0)
	assert.Contains(t, toRemove, 1)
	assert.Contains(t, toRemove, 2)
	assert.Contains(t, toRemove, 10)
	assert.Contains(t, toRemove, 11)
	assert.Contains(t, toRemove, 12)
	assert.Contains(t, toRemove, 20)
	assert.Contains(t, toRemove, 21)
	assert.Contains(t, toRemove, 22)
	assert.Equal(t, len(toRemove), 9)

	assert.Contains(t, toAdd, 44)
	assert.Contains(t, toAdd, 45)
	assert.Contains(t, toAdd, 46)
	assert.Contains(t, toAdd, 54)
	assert.Contains(t, toAdd, 55)
	assert.Contains(t, toAdd, 56)
	assert.Contains(t, toAdd, 64)
	assert.Contains(t, toAdd, 65)
	assert.Contains(t, toAdd, 66)
	assert.Equal(t, len(toAdd), 9)

	assert.Contains(t, curr, 44)
	assert.Contains(t, curr, 45)
	assert.Contains(t, curr, 46)
	assert.Contains(t, curr, 54)
	assert.Contains(t, curr, 55)
	assert.Contains(t, curr, 56)
	assert.Contains(t, curr, 64)
	assert.Contains(t, curr, 65)
	assert.Contains(t, curr, 66)
	assert.Equal(t, len(curr), 9)
}

func TestTileManager_CheckIntersection(t *testing.T) {

	locationInCenter := utils.Location{
		Latitude:  -0.5,
		Longitude: 0.5,
	}
	_, row, col, err := tm1.GetTileNrFromLocation(locationInCenter, false)
	if err != nil {
		t.Fatal(err)
	}

	topLeft := utils.Location{
		Latitude:  0.,
		Longitude: 0.,
	}

	botRight := utils.Location{
		Latitude:  -85.051150,
		Longitude: 180.000000,
	}

	_, rowtl, coltl, _ := tm1.GetTileNrFromLocation(topLeft, false)
	_, rowbr, colbr, _ := tm1.GetTileNrFromLocation(botRight, true)

	topLeft = utils.Location{
		Latitude:  float64(rowtl),
		Longitude: float64(coltl),
	}

	botRight = utils.Location{
		Latitude:  float64(rowbr),
		Longitude: float64(colbr),
	}

	fmt.Printf("[%f, %f]\n", botRight.Longitude, botRight.Longitude)
	fmt.Printf("[%f, %f]\n", topLeft.Longitude, topLeft.Longitude)

	tm1.exitBoundarySize = 1
	tm1.serverRect = locationUtils.LocationsToRect(topLeft, botRight)
	exitRect := tm1.CalculateCapForLocation(row, col, tm1.exitBoundarySize)

	fmt.Println(exitRect)

	tm1.cellsOwnedLock.RLock()
	if !exitRect.Intersects(tm1.serverRect) {
		tm1.cellsOwnedLock.RUnlock()
		fmt.Printf("[%f, %f] [%f, %f]\n", exitRect.X.Lo, exitRect.Y.Lo, exitRect.X.Hi, exitRect.Y.Hi)
		fmt.Printf("[%f, %f] [%f, %f]\n", tm1.serverRect.X.Lo, tm1.serverRect.Y.Lo, tm1.serverRect.X.Hi, tm1.serverRect.Y.Hi)
		t.Fatal("out of bounds of the server")
	}
	tm1.cellsOwnedLock.RUnlock()
}

/*func TestTileManager_GetTileBoundsFromTileNr(t *testing.T) {

	var (
		rm = NewCellManager(nil, 4, 100, 10,
			utils.Location{Latitude: LatitudeMax, Longitude: -180},
			utils.Location{Latitude: -LatitudeMax, Longitude: 180})
	)

	topLeft, botRight := cm.GetTileBoundsFromTileNr(0)

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

}*/
