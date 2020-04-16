package main

import (
	"encoding/json"
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/clients"
	locationdb "github.com/NOVAPokemon/utils/database/location"
	"github.com/NOVAPokemon/utils/gps"
	"github.com/NOVAPokemon/utils/tokens"
	ws "github.com/NOVAPokemon/utils/websockets"
	"github.com/NOVAPokemon/utils/websockets/location"
	locationMessages "github.com/NOVAPokemon/utils/websockets/messages/location"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"net/http"
	"sync"
	"time"
)

const (
	updateGymsInterval = 30 * time.Second
	locationVicinity   = 100

	exampleGymsFilename = "example_gyms.json"
)

var (
	gyms []utils.Gym
	lock sync.RWMutex
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
	inChan := make(chan *ws.Message)
	finish := make(chan struct{})

	go handleMessages(conn, inChan, finish)
	for {
		select {
		case <-pingTicker.C:
			if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		case msg := <-inChan:
			handleMsg(conn, user, msg)
			_ = conn.SetReadDeadline(time.Now().Add(location.Timeout))
		case <-finish:
			log.Warn("Stopped tracking location")
			return
		}
	}
}

func handleMessages(conn *websocket.Conn, channel chan *ws.Message, finished chan struct{}) {
	for {
		msg, err := clients.ReadMessagesWithoutParse(conn)
		if err != nil {
			log.Printf("error: %v", err)
			close(finished)
			return
		} else {
			channel <- msg
		}
	}
}

func handleMsg(conn *websocket.Conn, user string, msg *ws.Message) {
	switch msg.MsgType {
	case locationMessages.UpdateLocation:
		locationMsg := locationMessages.Deserialize(msg).(*locationMessages.UpdateLocationMessage)

		log.Info(user, " ", locationMsg.Location)

		_, err := locationdb.UpdateIfAbsentAddUserLocation(utils.UserLocation{
			Username: user,
			Location: locationMsg.Location,
		})
		if err != nil {
			log.Error(err)
			return
		}

		gymsInVicinity := getGymsInVicinity(locationMsg.Location)

		log.Info("gyms in vicinity: ", gymsInVicinity)

		if len(gymsInVicinity) > 0 {
			gymsMsgString := locationMessages.GymsMessage{Gyms: gymsInVicinity}.SerializeToWSMessage().Serialize()
			clients.Send(conn, &gymsMsgString)
		}
	default:
		log.Warn("invalid msg type")
	}
}

// TODO Discuss ideas to improve this. Maybe reduce the number of times this is calculated.
func getGymsInVicinity(location utils.Location) []utils.Gym {
	var gymsInVicinity []utils.Gym

	log.Info("gyms considered:", gyms)

	for _, gym := range gyms {
		distance := gps.CalcDistanceBetweenLocations(location, gym.Location)
		if distance <= locationVicinity {
			gymsInVicinity = append(gymsInVicinity, gym)
		}

		log.Infof("distance to %f: %f", gym.Location.Latitude, distance)
	}

	return gymsInVicinity
}

func updateGymsPeriodically() {
	lock.Lock()
	defer lock.Unlock()

	timer := time.NewTimer(updateGymsInterval)

	for {
		var err error
		gyms, err = locationdb.GetGyms()
		if err != nil {
			return
		}

		<-timer.C
		timer.Reset(updateGymsInterval)
	}
}

func loadExampleGyms() {
	if err := locationdb.DeleteAllGyms(); err != nil {
		log.Error(err)
		return
	}

	fileData, err := ioutil.ReadFile(exampleGymsFilename)
	if err != nil {
		log.Error(err)
		return
	}

	var gyms []utils.Gym
	err = json.Unmarshal(fileData, &gyms)
	if err != nil {
		log.Error(err)
		return
	}

	for _, gym := range gyms {
		if err := locationdb.AddGym(gym); err != nil {
			log.Error(err)
		}
	}
}
