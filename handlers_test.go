package main

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

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
	for serverNameAux, configAux := range configs {
		cellIds := convertCellTokensToIds(configAux.CellIdsStrings)

		for _, cell = range cells {
			if cellIds.ContainsCellID(cell) {
				serverAddr := fmt.Sprintf("%s.%s", serverNameAux, serviceNameHeadless)
				servers[serverAddr] = append(servers[serverAddr], cell)
			}
		}
	}

	fmt.Printf("%+v\n", servers)
}

func Test_CellManager_RandomPoint(t *testing.T) {
	rand.Seed(time.Now().Unix())

	trainerCellId := s2.CellFromLatLng(s2.LatLngFromDegrees(0.5, 0.5)).ID().Parent(18)

	trainerCell := s2.CellFromCellID(trainerCellId)
	toGenerate := 20
	fmt.Printf("Generating %d pokemons for cell %d\n", toGenerate, trainerCell.ID())

	maxTriesPerCell := 50
	tries := 0

	for numGenerated := 0; numGenerated < toGenerate && tries < maxTriesPerCell; {
		cellRect := trainerCell.RectBound()
		fmt.Printf("rectangle bounds : %+v\n", cellRect)

		fmt.Printf("rectLatLng: %f, %f", cellRect.Lat, cellRect.Lng.Lo)

		randLat := cellRect.Lat.Lo + (cellRect.Lat.Hi-cellRect.Lat.Lo)*rand.Float64()
		randLng := cellRect.Lng.Lo + (cellRect.Lng.Hi-cellRect.Lng.Lo)*rand.Float64()
		fmt.Printf("Lat Lng: %f, %f\n", randLat, randLng)
		randomLatLng := s2.LatLng{
			Lat: s1.Angle(randLat),
			Lng: s1.Angle(randLng),
		}
		fmt.Printf("randomLatLng: %f, %f\n", randomLatLng.Lat.Degrees(), randomLatLng.Lng.Degrees())

		randomCellId := s2.CellFromLatLng(randomLatLng).ID().Parent(20)
		randomCell := s2.CellFromCellID(randomCellId)

		fmt.Printf("checking if cell %d contains cell %d\n", trainerCell.ID(), randomCell.ID())

		if trainerCell.ContainsCell(randomCell) {
			numGenerated++
			fmt.Printf("Added wild pokemon to cellId: %d\n", randomCellId)
			fmt.Println("---------------------------------------------------------")
			tries = 0
		} else {
			tries++
			fmt.Println("randomized point for pokemon generation ended up outside of boundaries")
		}
	}

	if tries == maxTriesPerCell {
		t.Fatal("tried to many times")
	} else {
		fmt.Println("was able to calculate")
	}
}

func TestCellsContainment(t *testing.T) {
	c1 := s2.CellFromCellID(1153010638079918080)
	c2 := s2.CellFromCellID(1152921504606846976)

	fmt.Println(c2.Level())

	if c1.ContainsCell(c2) {
		fmt.Println("it contains")
	} else {
		t.Fatal("did not contain")
	}
}
