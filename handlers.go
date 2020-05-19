package main

import (
	"encoding/json"
	"errors"
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
	"github.com/NOVAPokemon/utils/clients"
	locationdb "github.com/NOVAPokemon/utils/database/location"
	"github.com/NOVAPokemon/utils/tokens"
	ws "github.com/NOVAPokemon/utils/websockets"
	"github.com/NOVAPokemon/utils/websockets/location"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	maxCatchingProbability = 100
)

var (
	tm                *TileManager
	serverName        string
	serverNr          int64
	timeoutInDuration = time.Duration(config.Timeout) * time.Second
	tmLock            = sync.RWMutex{}
	httpClient        = &http.Client{}
)

func init() {
	if aux, exists := os.LookupEnv(utils.HostnameEnvVar); exists {
		serverName = aux
	} else {
		log.Fatal("Could not load server name")
	}
	log.Info("Server name : ", serverName)
	if serverNrTmp, err := strconv.ParseInt(serverName[:len(serverName)-1], 10, 32); err != nil {
		serverNr = serverNrTmp
	}

	gyms, err := locationdb.GetGyms()
	if err != nil {
		log.Fatal(err)
	}

	serverConfig, err := locationdb.GetServerConfig(serverName)
	if err != nil {
		if serverNr == 0 {
			// if configs are missing, server 0 adds them
			err := insertDefaultBoundariesInDB()
			if err != nil {
				log.Fatal(WrapInit(err))
			}
		} else {
			log.Error(WrapInit(err))
			log.Warnf("Starting with default config: %+v", config)
		}
		tmLock.Lock()
		tm = NewTileManager(gyms, config.NumTilesInWorld, config.MaxPokemonsPerTile, config.NumberOfPokemonsToGenerate,
			config.TopLeftCorner, config.BotRightCorner)
		tmLock.Unlock()
	} else {
		log.Warnf("Loaded config: %+v", serverConfig)
		tmLock.Lock()
		tm = NewTileManager(gyms, config.NumTilesInWorld, config.MaxPokemonsPerTile, config.NumberOfPokemonsToGenerate,
			serverConfig.TopLeftCorner, serverConfig.BotRightCorner)
		tmLock.Unlock()
	}
	go RefreshBoundariesPeriodic()
}

func insertDefaultBoundariesInDB() error {
	data, err := ioutil.ReadFile(DefaultServerBoundariesFile)
	if err != nil {
		return WrapLoadServerBoundaries(err)
	}

	var boundaryConfigs []utils.LocationServerBoundary
	err = json.Unmarshal(data, &boundaryConfigs)
	if err != nil {
		return WrapLoadServerBoundaries(err)
	}

	for i := 0; i < len(boundaryConfigs); i++ {
		if err := locationdb.UpdateServerConfig(boundaryConfigs[i].ServerName, boundaryConfigs[i]); err != nil {
			log.Warn(WrapLoadServerBoundaries(err))
		}
	}
	return nil
}

func RefreshBoundariesPeriodic() {
	for {
		serverConfig, err := locationdb.GetServerConfig(serverName)
		if err != nil {
			log.Error(err)
		} else {
			log.Infof("Loaded config: %+v", *serverConfig)
			tmLock.RLock()
			tm.SetBoundaries(serverConfig.TopLeftCorner, serverConfig.BotRightCorner)
			tmLock.RLock()
		}
		time.Sleep(time.Duration(config.UpdateConfigsInterval) * time.Second)
	}
}

func HandleAddGymLocation(w http.ResponseWriter, r *http.Request) {
	var gym utils.Gym
	err := json.NewDecoder(r.Body).Decode(&gym)
	if err != nil {
		log.Error(wrapAddGymError(err))
		return
	}

	tmLock.RLock()
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
	tmLock.RUnlock()
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

func HandleSetServerConfigs(w http.ResponseWriter, r *http.Request) {
	config := &utils.LocationServerBoundary{}
	servername := mux.Vars(r)[api.ServerNamePathVar]
	err := json.NewDecoder(r.Body).Decode(config)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapSetServerConfigsError(err), http.StatusBadRequest)
		return
	}
	err = locationdb.UpdateServerConfig(servername, *config)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapSetServerConfigsError(err), http.StatusInternalServerError)
		return
	}
	setServerRegionConfig(config)
}

func HandleGetGlobalRegionSettings(w http.ResponseWriter, _ *http.Request) {
	serverConfigs, err := locationdb.GetAllServerConfigs()
	if err != nil {
		log.Fatal(err)
	}
	toSend, err := json.Marshal(serverConfigs)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapGetAllConfigs(err), http.StatusInternalServerError)
	}
	_, _ = w.Write(toSend)
}

func HandleGetServerForLocation(w http.ResponseWriter, r *http.Request) {
	latStr := r.FormValue(api.LatitudeQueryParam)
	lonStr := r.FormValue(api.LongitudeQueryParam)

	log.Infof("Request to determine location for Lat: %s Lon: %s", latStr, lonStr)
	lat, err := strconv.ParseFloat(latStr, 32)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapGetServerForLocation(err), http.StatusBadRequest)
		return
	}

	lon, err := strconv.ParseFloat(lonStr, 32)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapGetServerForLocation(err), http.StatusBadRequest)
		return
	}

	loc := utils.Location{
		Latitude:  lat,
		Longitude: lon,
	}
	configs, err := locationdb.GetAllServerConfigs() // TODO Can be optimized, instead of fetching all the configs and looping
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapGetServerForLocation(err), http.StatusInternalServerError)
		return
	}

	log.Info("Configs: ", configs)

	for serverName, config := range configs {
		if isWithinBounds(loc, config.TopLeftCorner, config.BotRightCorner) {
			log.Info("Server found")
			toSend, err := json.Marshal(serverName)
			if err != nil {
				utils.LogAndSendHTTPError(&w, wrapGetServerForLocation(err), http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(toSend)
			return
		}
	}
	utils.LogAndSendHTTPError(&w, wrapGetServerForLocation(errors.New("no server found for supplied location")), http.StatusNotFound)
}

func handleUserLocationUpdates(user string, conn *websocket.Conn) {
	defer ws.CloseConnection(conn)

	_ = conn.SetReadDeadline(time.Now().Add(timeoutInDuration))
	conn.SetPongHandler(func(string) error {
		//log.Warn("Received pong")
		_ = conn.SetReadDeadline(time.Now().Add(timeoutInDuration))
		return nil
	})

	inChan := make(chan *ws.Message)
	finish := make(chan struct{})

	go handleMessagesLoop(conn, inChan, finish)
	defer func() {
		if err := tm.RemoveTrainerLocation(user); err != nil {
			log.Error(err)
		}
	}()

	var pingTicker = time.NewTicker(time.Duration(config.Ping) * time.Second)
	for {
		select {
		case msg, ok := <-inChan:
			if !ok {
				continue
			}
			err := handleLocationMsg(conn, user, msg)
			if err != nil {
				log.Error(ws.WrapWritingMessageError(err))
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(timeoutInDuration))
		case <-pingTicker.C:
			if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				log.Error(ws.WrapWritingMessageError(err))
				return
			}
			//log.Warn("Pinging")
		case <-finish:
			log.Infof("Stopped tracking user %s location", user)
			return
		}
	}
}

func handleMessagesLoop(conn *websocket.Conn, channel chan *ws.Message, finished chan struct{}) {
	defer close(channel)
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
		desMsg, err := location.DeserializeLocationMsg(msg)
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

func setServerRegionConfig(serverConfig *utils.LocationServerBoundary) {
	tmLock.Lock()
	tm.SetBoundaries(serverConfig.TopLeftCorner, serverConfig.BotRightCorner)
	tmLock.Unlock()
}

func HandleForceLoadConfig(_ http.ResponseWriter, _ *http.Request) {
	serverConfig, err := locationdb.GetServerConfig(serverName)
	if err != nil {
		log.Fatal(err)
	}
	setServerRegionConfig(serverConfig)
}
