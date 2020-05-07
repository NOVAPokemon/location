package main

import (
	"encoding/json"
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
	tm *TileManager

	timeoutInDuration = time.Duration(config.Timeout) * time.Second
	tmLock            = sync.RWMutex{}
	httpClient        = &http.Client{}
)

func init() {
	gyms, err := locationdb.GetGyms()
	if err != nil {
		log.Fatal(err)
	}

	tmLock.Lock()
	tm = NewTileManager(gyms, config.NumTilesInWorld, config.MaxPokemonsPerTile, config.NumberOfPokemonsToGenerate,
		config.TopLeftCorner, config.BotRightCorner)
	tmLock.Unlock()
}

func HandleAddGymLocation(w http.ResponseWriter, r *http.Request) {
	var gym utils.Gym
	err := json.NewDecoder(r.Body).Decode(&gym)
	if err != nil {
		log.Error(wrapAddGymError(err))
		return
	}

	err = tm.AddGym(gym)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapAddGymError(err), http.StatusInternalServerError)
		return
	}

	err = locationdb.AddGym(gym)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapAddGymError(err), http.StatusInternalServerError)
		return
	}

	err = tm.AddGym(gym)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapAddGymError(err), http.StatusInternalServerError)
		return
	}
}

func HandleUserLocation(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		err = wrapUserLocationError(ws.WrapUpgradeConnectionError(err))
		utils.LogAndSendHTTPError(&w, err, http.StatusUnauthorized)
		return
	}

	authToken, err := tokens.ExtractAndVerifyAuthToken(r.Header)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapUserLocationError(err), http.StatusUnauthorized)
		return
	}

	handleUserLocationUpdates(authToken.Username, conn)
}

func HandleSetArea(w http.ResponseWriter, _ *http.Request) {
	gyms, err := locationdb.GetGyms()
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapSetAreaError(err), http.StatusInternalServerError)
		return
	}

	tmLock.Lock()
	tm = NewTileManager(gyms, config.NumTilesInWorld, config.MaxPokemonsPerTile, config.NumberOfPokemonsToGenerate,
		config.TopLeftCorner, config.BotRightCorner)
	tmLock.Unlock()
}

func HandleCatchWildPokemon(w http.ResponseWriter, r *http.Request) {
	authToken, err := tokens.ExtractAndVerifyAuthToken(r.Header)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapCatchWildPokemonError(err), http.StatusUnauthorized)
		return
	}

	var request api.CatchWildPokemonRequest
	err = json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapCatchWildPokemonError(err), http.StatusInternalServerError)
		return
	}

	pokeball := request.Pokeball
	if !pokeball.IsPokeBall() {
		err = wrapCatchWildPokemonError(errorInvalidItemCatch)
		utils.LogAndSendHTTPError(&w, err, http.StatusInternalServerError)
		return
	}

	regionNr, ok := tm.GetTrainerTile(authToken.Username)
	if !ok {
		err = wrapCatchWildPokemonError(errorLocationNotTracked)
		utils.LogAndSendHTTPError(&w, err, http.StatusBadRequest)
		return
	}

	pokemon, err := tm.RemoveWildPokemonFromTile(regionNr, request.Pokemon.Id.Hex())
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapCatchWildPokemonError(err), http.StatusInternalServerError)
		return
	}

	if !ok {
		err = wrapCatchWildPokemonError(newPokemonNotAvailable(pokemon.Id.Hex()))
		utils.LogAndSendHTTPError(&w, err, http.StatusBadRequest)
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
		utils.LogAndSendHTTPError(&w, wrapCatchWildPokemonError(err), http.StatusInternalServerError)
		return
	}

	if !caught {
		_, err = w.Write(jsonBytes)
		if err != nil {
			utils.LogAndSendHTTPError(&w, wrapCatchWildPokemonError(err), http.StatusInternalServerError)
		}
		return
	}

	log.Info(authToken.Username, " caught: ", caught)
	var trainersClient = clients.NewTrainersClient(httpClient)
	_, err = trainersClient.AddPokemonToTrainer(authToken.Username, *pokemon)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapCatchWildPokemonError(err), http.StatusInternalServerError)
		return
	}

	pokemonTokens := make([]string, 0, len(trainersClient.PokemonTokens))
	for _, tokenString := range trainersClient.PokemonTokens {
		pokemonTokens = append(pokemonTokens, tokenString)
	}

	w.Header()[tokens.PokemonsTokenHeaderName] = pokemonTokens
	_, err = w.Write(jsonBytes)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapCatchWildPokemonError(err), http.StatusInternalServerError)
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
			err := handleLocationMsg(conn, user, msg)
			if err != nil {
				log.Error(err)
			}

			_ = conn.SetReadDeadline(time.Now().Add(timeoutInDuration))
		case <-pingTicker.C:
			if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				log.Error(ws.WrapWritingMessageError(err))
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
		msg, err := clients.Read(conn)
		if err != nil {
			log.Error(err)
			close(finished)
			return
		} else {
			channel <- msg
		}
	}
}

func handleLocationMsg(conn *websocket.Conn, user string, msg *ws.Message) error {
	switch msg.MsgType {
	case location.UpdateLocation:
		desMsg, err := location.Deserialize(msg)
		if err != nil {
			return wrapHandleLocationMsgs(err)
		}

		locationMsg := desMsg.(*location.UpdateLocationMessage)
		tmLock.RLock()

		regionNr, err := tm.SetTrainerLocation(user, locationMsg.Location)
		if err != nil {
			return wrapHandleLocationMsgs(err)
		}

		gymsInVicinity := tm.getGymsInTile(regionNr)
		pokemonInVicinity, err := tm.getPokemonsInTile(regionNr)
		if err != nil {
			return wrapHandleLocationMsgs(err)
		}

		tmLock.RUnlock()

		gymsMsgString := location.GymsMessage{Gyms: gymsInVicinity}.SerializeToWSMessage().Serialize()

		err = clients.Send(conn, &gymsMsgString)
		if err != nil {
			return wrapHandleLocationMsgs(err)
		}

		pokemonMsgString := location.PokemonMessage{
			Pokemon: pokemonInVicinity,
		}.SerializeToWSMessage().Serialize()

		err = clients.Send(conn, &pokemonMsgString)
		if err != nil {
			return wrapHandleLocationMsgs(err)
		}

		return nil
	default:
		return wrapHandleLocationMsgs(ws.ErrorInvalidMessageType)
	}
}
