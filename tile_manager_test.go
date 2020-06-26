package main

import (
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/NOVAPokemon/utils"
	locationUtils "github.com/NOVAPokemon/utils/location"
	"github.com/stretchr/testify/assert"
)

/*func TestMain(m *testing.M) {
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
}*/

var (
	numTiles        = 100
	numTilesPerAxis = int(math.Sqrt(float64(numTiles)))
	tileSide        = 360.0 / float64(numTilesPerAxis)
	tm1             = &TileManager{
		numTilesInWorld:          numTiles,
		activeTiles:              sync.Map{},
		trainerTiles:             sync.Map{},
		numTilesPerAxis:          numTilesPerAxis,
		tileSideLength:           tileSide,
		maxPokemonsPerTile:       100,
		maxPokemonsPerGeneration: 10,
		activateTileLock:         sync.Mutex{},
	}

	locationInCenter = utils.Location{
		Latitude:  -LatitudeMax,
		Longitude: -180,
	}
)

func TestTileManager_NEW_GetTileNrFromLocation(t *testing.T) {
	_, row, col, err := tm1.GetTileNrFromLocation(locationInCenter)
	if err != nil {
		// t.Fatal(err)
	}

	fmt.Println(row)
	fmt.Println(col)

	tileNrs := tm1.getBoundaryTiles(row, col, 1)

	assert.Contains(t, tileNrs, 44)
	assert.Contains(t, tileNrs, 45)
	assert.Contains(t, tileNrs, 46)
	assert.Contains(t, tileNrs, 54)
	assert.Contains(t, tileNrs, 55)
	assert.Contains(t, tileNrs, 56)
	assert.Contains(t, tileNrs, 64)
	assert.Contains(t, tileNrs, 65)
	assert.Contains(t, tileNrs, 66)
	assert.Equal(t, len(tileNrs), 9)
}

func TestTileManager_CalculateTileChanges(t *testing.T) {
	_, row, col, err := tm1.GetTileNrFromLocation(locationInCenter)
	if err != nil {
		t.Fatal(err)
	}

	trainerId := "teste"
	tm1.trainerTiles.Store(trainerId, []int{0, 1, 2, 10, 11, 12, 20, 21, 22})
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
	_, row, col, err := tm1.GetTileNrFromLocation(locationInCenter)
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

	_, rowtl, coltl, _ := tm1.GetTileNrFromLocation(topLeft)
	_, rowbr, colbr, _ := tm1.GetTileNrFromLocation(botRight)

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
	exitRect := tm1.CalculateBoundaryForLocation(row, col, tm1.exitBoundarySize)

	fmt.Println(exitRect)

	tm1.boundariesLock.RLock()
	if !exitRect.Intersects(tm1.serverRect) {
		tm1.boundariesLock.RUnlock()
		fmt.Printf("[%f, %f] [%f, %f]\n", exitRect.X.Lo, exitRect.Y.Lo, exitRect.X.Hi, exitRect.Y.Hi)
		fmt.Printf("[%f, %f] [%f, %f]\n", tm1.serverRect.X.Lo, tm1.serverRect.Y.Lo, tm1.serverRect.X.Hi, tm1.serverRect.Y.Hi)
		t.Fatal("out of bounds of the server")
	}
	tm1.boundariesLock.RUnlock()
}

/*func TestTileManager_GetTileBoundsFromTileNr(t *testing.T) {

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

}*/
