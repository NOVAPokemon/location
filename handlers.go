package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
	"github.com/NOVAPokemon/utils/clients"
	locationdb "github.com/NOVAPokemon/utils/database/location"
	"github.com/NOVAPokemon/utils/tokens"
	ws "github.com/NOVAPokemon/utils/websockets"
	"github.com/NOVAPokemon/utils/websockets/location"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

const (
	maxCatchingProbability = 100
)

var (
	timeoutInDuration = time.Duration(config.Timeout) * time.Second
	tm                *TileManager

	tmLock     = sync.RWMutex{}
	httpClient = &http.Client{}
)

func init() {
	gyms, err := locationdb.GetGyms()
	if err != nil {
		panic(err)
	}
	tmLock.Lock()
	tm = NewTileManager(gyms, config.NumTilesInWorld, config.MaxPokemonsPerTile, config.NumberOfPokemonsToGenerate, config.TopLeftCorner, config.BotRightCorner)
	tmLock.Unlock()
}

func HandleAddGymLocation(w http.ResponseWriter, r *http.Request) {
	var gym utils.Gym
	err := json.NewDecoder(r.Body).Decode(&gym)
	if err != nil {
		log.Error(err)
		return
	}

	if err = tm.AddGym(gym); err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	if err := locationdb.AddGym(gym); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if err = tm.AddGym(gym); err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func HandleUserLocation(w http.ResponseWriter, r *http.Request) {
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

	handleUserLocationUpdates(authToken.Username, conn)
}

func HandleSetArea(_ http.ResponseWriter, _ *http.Request) {
	gyms, err := locationdb.GetGyms()
	if err != nil {
		panic(err)
	}
	tmLock.Lock()
	tm = NewTileManager(gyms, config.NumTilesInWorld, config.MaxPokemonsPerTile, config.NumberOfPokemonsToGenerate, config.TopLeftCorner, config.BotRightCorner)
	tmLock.Unlock()
}

func HandleCatchWildPokemon(w http.ResponseWriter, r *http.Request) {
	authToken, err := tokens.ExtractAndVerifyAuthToken(r.Header)
	if err != nil {
		log.Error("no auth token")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var request api.CatchWildPokemonRequest
	err = json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		log.Error(err)
		return
	}

	pokeball := request.Pokeball
	if !pokeball.IsPokeBall() {
		log.Error("invalid item to catch")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	regionNr, ok := tm.GetTrainerTile(authToken.Username)

	if !ok {
		log.Error("location not being tracked")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	pokemon, err := tm.RemoveWildPokemonFromTile(regionNr, request.Pokemon.Id.Hex())
	if err != nil {
		log.Error(err)
		return
	}

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		log.Error(errors.New(fmt.Sprintf("pokemon %s is not available to catch", pokemon.Id.Hex())))
		return
	}

	var catchingProbability float64
	if pokeball.Effect.Value == maxCatchingProbability {
		catchingProbability = 1
	} else {
		catchingProbability = 1 - ((float64(pokemon.Level) / config.MaxLevel) *
			(float64(pokeball.Effect.Value) / maxCatchingProbability))
	}

	log.Info("catching probability: ", catchingProbability)

	caught := rand.Float64() <= catchingProbability
	caughtMessage := clients.CaughtPokemonMessage{
		Caught: caught,
	}

	jsonBytes, err := json.Marshal(caughtMessage)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if !caught {
		_, err = w.Write(jsonBytes)
		if err != nil {
			log.Error(err)
		}
		return
	}

	log.Info(authToken.Username, " caught: ", caught)
	var trainersClient = clients.NewTrainersClient(httpClient)
	_, err = trainersClient.AddPokemonToTrainer(authToken.Username, *pokemon)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	pokemonTokens := make([]string, 0, len(trainersClient.PokemonTokens))
	for _, tokenString := range trainersClient.PokemonTokens {
		pokemonTokens = append(pokemonTokens, tokenString)
	}

	w.Header()[tokens.PokemonsTokenHeaderName] = pokemonTokens
	_, err = w.Write(jsonBytes)
	if err != nil {
		log.Error(err)
	}
}

func handleUserLocationUpdates(user string, conn *websocket.Conn) {
	defer ws.CloseConnection(conn)

	_ = conn.SetReadDeadline(time.Now().Add(timeoutInDuration))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(timeoutInDuration))
		return nil
	})

	var pingTicker = time.NewTicker(time.Duration(config.Ping) * time.Second)
	inChan := make(chan *ws.Message)
	finish := make(chan struct{})

	go handleMessagesLoop(conn, inChan, finish)
	for {
		select {
		case msg := <-inChan:
			handleMsg(conn, user, msg)
			_ = conn.SetReadDeadline(time.Now().Add(timeoutInDuration))
		case <-pingTicker.C:
			if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				log.Error(err)
				return
			}
		case <-finish:
			log.Info("Stopped tracking location")
			return
		}
	}
}

func handleMessagesLoop(conn *websocket.Conn, channel chan *ws.Message, finished chan struct{}) {
	for {
		msg, err := clients.ReadMessagesWithoutParse(conn)
		if err != nil {
			log.Error(err)
			close(finished)
			return
		} else {
			channel <- msg
		}
	}
}

func handleMsg(conn *websocket.Conn, user string, msg *ws.Message) {
	switch msg.MsgType {
	case location.UpdateLocation:
		locationMsg := location.Deserialize(msg).(*location.UpdateLocationMessage)
		tmLock.RLock()
		regionNr, err := tm.SetTrainerLocation(user, locationMsg.Location)

		if err != nil {
			log.Error(err)
			return
		}
		gymsInVicinity := tm.getGymsInTile(regionNr)
		pokemonInVicinity, err := tm.getPokemonsInTile(regionNr)
		if err != nil {
			log.Error(err)
			return
		}
		tmLock.RUnlock()
		gymsMsgString := location.GymsMessage{Gyms: gymsInVicinity}.SerializeToWSMessage().Serialize()
		clients.Send(conn, &gymsMsgString)

		pokemonMsgString := location.PokemonMessage{
			Pokemon: pokemonInVicinity,
		}.SerializeToWSMessage().Serialize()

		clients.Send(conn, &pokemonMsgString)

	default:
		log.Warn("invalid msg type")
	}
}
