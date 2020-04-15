package main

import (
	"encoding/json"
	"github.com/NOVAPokemon/utils"
	locationdb "github.com/NOVAPokemon/utils/database/location"
	"github.com/NOVAPokemon/utils/tokens"
	"github.com/NOVAPokemon/utils/websockets/location"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"net/http"
	"time"
)

func handleAddGymLocation(w http.ResponseWriter, r *http.Request) {
	var gym utils.Gym
	err := json.NewDecoder(r.Body).Decode(&gym)
	if err != nil {
		log.Error(err)
		return
	}

	err = locationdb.AddGym(gym)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

}

func handleUserLocation(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	authToken, err := tokens.ExtractAndVerifyAuthToken(r.Header)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	go handleUserLocationUpdates(authToken.Username, conn)
}

func handleUserLocationUpdates(user string, conn *websocket.Conn) {
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(location.Timeout))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(location.Timeout))
		return nil
	})

	var pingTicker = time.NewTicker(location.PingCooldown)
	inChan := make(chan utils.Location)
	finish := make(chan *struct{})

	go handleLocationMessages(conn, inChan, finish)
	for {
		select {
		case <-pingTicker.C:
			if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		case loc := <-inChan:
			_, err := locationdb.UpdateIfAbsentAddUserLocation(user, loc)
			if err != nil {
				log.Error(err)
				return
			}

			_ = conn.SetReadDeadline(time.Now().Add(location.Timeout))
		case <-finish:
			log.Warn("Stopped tracking location")
			return
		}
	}
}

func handleLocationMessages(conn *websocket.Conn, channel chan utils.Location, finished chan *struct{}) {
	for {
		loc := utils.Location{}
		err := conn.ReadJSON(&loc)
		if err != nil {
			log.Printf("error: %v", err)
			finished <- nil
			return
		} else {
			channel <- loc
		}
	}
}
