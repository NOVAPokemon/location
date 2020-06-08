package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

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
)

type (
	valueType = chan ws.GenericMsg
)

const (
	maxCatchingProbability = 100
)

var (
	tm                  *TileManager
	serverName          string
	serverNr            int64
	timeoutInDuration   = time.Duration(config.Timeout) * time.Second
	httpClient          = &http.Client{}
	clientChannels      = sync.Map{}
	serviceNameHeadless string
)

func init() {

	if aux, exists := os.LookupEnv(utils.HeadlessServiceNameEnvVar); exists {
		serviceNameHeadless = aux
	} else {
		log.Fatal("Could not load headless service name")
	}

	if aux, exists := os.LookupEnv(utils.HostnameEnvVar); exists {
		serverName = aux
	} else {
		log.Fatal(WrapInit(errors.New("could not load server name")))
	}
	log.Info("Server name : ", serverName)

	split := strings.Split(serverName, "-")
	if serverNrTmp, err := strconv.ParseInt(split[len(split)-1], 10, 32); err != nil {
		log.Fatal(WrapInit(err))
	} else {
		serverNr = serverNrTmp
	}

	for i := 0; i < 5; i++ {
		time.Sleep(time.Duration(5*i) * time.Second)
		serverConfig, err := locationdb.GetServerConfig(serverName)
		if err != nil {
			if serverNr == 0 {
				// if configs are missing, server 0 adds them
				err := insertDefaultBoundariesInDB()
				if err != nil {
					log.Error(WrapInit(err))
				}
			}
			log.Error(WrapInit(err))
		} else {
			gyms, err := locationdb.GetGyms()
			if err != nil {
				log.Error(err)
				continue
			}

			log.Infof("Loaded config: %+v", serverConfig)
			tm = NewTileManager(gyms, config.NumTilesInWorld, config.MaxPokemonsPerTile, config.NumberOfPokemonsToGenerate,
				serverConfig.TopLeftCorner, serverConfig.BotRightCorner)
			go RefreshBoundariesPeriodic()
			go refreshGymsPeriodic()
			return
		}
	}
	log.Panic(WrapInit(errors.New("could not load configs from DB")))
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

func refreshGymsPeriodic() {
	for {
		time.Sleep(time.Duration(config.UpdateGymsInterval) * time.Second)
		gyms, err := locationdb.GetGyms()
		if err != nil {
			log.Error(err)
			continue
		}
		if err := tm.SetGyms(gyms); err != nil {
			log.Error(err)
			continue
		}
	}
}

func RefreshBoundariesPeriodic() {
	for {
		serverConfig, err := locationdb.GetServerConfig(serverName)
		if err != nil {
			log.Error(err)
		} else {
			log.Infof("Loaded boundaries: %+v", *serverConfig)
			tm.SetBoundaries(serverConfig.TopLeftCorner, serverConfig.BotRightCorner)
		}
		time.Sleep(time.Duration(config.UpdateConfigsInterval) * time.Second)
	}
}

func HandleAddGymLocation(w http.ResponseWriter, r *http.Request) {
	var gym utils.GymWithServer
	err := json.NewDecoder(r.Body).Decode(&gym)
	if err != nil {
		log.Error(wrapAddGymError(err))
		return
	}

	err = locationdb.UpdateIfAbsentAddGym(gym)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapAddGymError(err), http.StatusInternalServerError)
		return
	}

	if isWithinBounds(gym.Gym.Location, tm.TopLeftCorner, tm.BotRightCorner) {
		err = tm.AddGym(gym)
		if err != nil {
			log.Error(wrapAddGymError(err))
			return
		}
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
	for serverName, config := range configs {
		if isWithinBounds(loc, config.TopLeftCorner, config.BotRightCorner) {
			serverAddr := fmt.Sprintf("%s.%s", serverName, serviceNameHeadless)
			toSend, err := json.Marshal(serverAddr)
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
		// log.Warn("Received pong")
		_ = conn.SetReadDeadline(time.Now().Add(timeoutInDuration))
		return nil
	})

	inChan := make(chan *ws.Message)
	outChan := make(chan ws.GenericMsg)
	finish := make(chan struct{})

	clientChannels.Store(user, outChan)
	defer clientChannels.Delete(user)

	go handleMessagesLoop(conn, inChan, finish)
	go handleWriteLoop(conn, outChan, finish)
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
			err := handleLocationMsg(user, msg)
			if err != nil {
				log.Error(ws.WrapWritingMessageError(err))
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(timeoutInDuration))
		case <-pingTicker.C:
			outChan <- ws.GenericMsg{
				MsgType: websocket.PingMessage,
				Data:    []byte{},
			}
			// log.Warn("Pinging")
		case <-finish:
			log.Infof("Stopped tracking user %s location", user)
			return
		}
	}
}

func handleWriteLoop(conn *websocket.Conn, channel chan ws.GenericMsg, finished chan struct{}) {
	defer close(channel)
	for {
		select {
		case msg := <-channel:
			// log.Info("Sending ", msg.MsgType, string(msg.Data))
			if err := conn.WriteMessage(msg.MsgType, msg.Data); err != nil {
				log.Error(ws.WrapWritingMessageError(err))
			}
		case <-finished:
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

func handleLocationMsg(user string, msg *ws.Message) error {
	switch msg.MsgType {
	case location.CatchPokemon:
		desMsg, err := location.DeserializeLocationMsg(msg)
		if err != nil {
			return wrapHandleLocationMsgs(err)
		}
		catchPokemonMsg := desMsg.(*location.CatchWildPokemonMessage)
		pokeball := catchPokemonMsg.Pokeball
		if !pokeball.IsPokeBall() {
			msgBytes := []byte(ws.ErrorMessage{
				Info:  wrapCatchWildPokemonError(errorInvalidItemCatch).Error(),
				Fatal: false,
			}.SerializeToWSMessage().Serialize())
			channelGeneric, ok := clientChannels.Load(user)
			if !ok {
				return nil
			}

			channel := channelGeneric.(valueType)
			channel <- ws.GenericMsg{
				MsgType: websocket.TextMessage,
				Data:    msgBytes,
			}

			return nil
		}

		regionNrInterface, ok := tm.GetTrainerTile(user)
		if !ok {
			msgBytes := []byte(ws.ErrorMessage{
				Info:  wrapCatchWildPokemonError(errorLocationNotTracked).Error(),
				Fatal: true,
			}.SerializeToWSMessage().Serialize())

			channelGeneric, ok := clientChannels.Load(user)
			if !ok {
				return nil
			}

			channel := channelGeneric.(valueType)
			channel <- ws.GenericMsg{
				MsgType: websocket.TextMessage,
				Data:    msgBytes,
			}
			return err
		}

		regionNr := regionNrInterface.(trainerTileValueType)

		pokemon, err := tm.RemoveWildPokemonFromTile(regionNr, catchPokemonMsg.Pokemon)
		if err != nil {
			msgBytes := []byte(ws.ErrorMessage{
				Info:  wrapCatchWildPokemonError(err).Error(),
				Fatal: false,
			}.SerializeToWSMessage().Serialize())

			channelGeneric, ok := clientChannels.Load(user)
			if !ok {
				return nil
			}

			channel := channelGeneric.(valueType)
			channel <- ws.GenericMsg{
				MsgType: websocket.TextMessage,
				Data:    msgBytes,
			}
			return nil
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
		var pokemonTokens []string
		if caught {
			var trainersClient = clients.NewTrainersClient(httpClient)
			_, err = trainersClient.AddPokemonToTrainer(user, *pokemon)
			if err != nil {
				msgBytes := []byte(ws.ErrorMessage{
					Info:  wrapHandleLocationMsgs(err).Error(),
					Fatal: true,
				}.SerializeToWSMessage().Serialize())

				channelGeneric, ok := clientChannels.Load(user)
				if !ok {
					return nil
				}

				channel := channelGeneric.(valueType)
				channel <- ws.GenericMsg{
					MsgType: websocket.TextMessage,
					Data:    msgBytes,
				}
				return err
			}
			pokemonTokens = make([]string, 0, len(trainersClient.PokemonTokens))
			for _, tokenString := range trainersClient.PokemonTokens {
				pokemonTokens = append(pokemonTokens, tokenString)
			}
		}

		msgBytes := []byte(location.CatchWildPokemonMessageResponse{
			Caught:        caught,
			PokemonTokens: pokemonTokens,
		}.SerializeToWSMessage().Serialize())

		channelGeneric, ok := clientChannels.Load(user)
		if !ok {
			return nil
		}

		channel := channelGeneric.(valueType)
		channel <- ws.GenericMsg{
			MsgType: websocket.TextMessage,
			Data:    msgBytes,
		}
		return nil

	case location.UpdateLocation:
		desMsg, err := location.DeserializeLocationMsg(msg)
		if err != nil {
			return wrapHandleLocationMsgs(err)
		}

		locationMsg := desMsg.(*location.UpdateLocationMessage)
		regionNr, err := tm.SetTrainerLocation(user, locationMsg.Location)
		if err != nil {
			return wrapHandleLocationMsgs(err)
		}

		gymsInVicinity := tm.getGymsInTile(regionNr)
		pokemonInVicinity, err := tm.getPokemonsInTile(regionNr)
		if err != nil {
			return wrapHandleLocationMsgs(err)
		}
		channelGeneric, ok := clientChannels.Load(user)
		if !ok {
			return nil
		}

		channel := channelGeneric.(valueType)
		channel <- ws.GenericMsg{
			MsgType: websocket.TextMessage,
			Data:    []byte(location.GymsMessage{Gyms: gymsInVicinity}.SerializeToWSMessage().Serialize()),
		}

		channel <- ws.GenericMsg{
			MsgType: websocket.TextMessage,
			Data: []byte(location.PokemonMessage{
				Pokemon: pokemonInVicinity,
			}.SerializeToWSMessage().Serialize()),
		}

		return nil
	default:
		return wrapHandleLocationMsgs(ws.ErrorInvalidMessageType)
	}
}

func setServerRegionConfig(serverConfig *utils.LocationServerBoundary) {
	tm.SetBoundaries(serverConfig.TopLeftCorner, serverConfig.BotRightCorner)
}

func HandleForceLoadConfig(_ http.ResponseWriter, _ *http.Request) {
	serverConfig, err := locationdb.GetServerConfig(serverName)
	if err != nil {
		log.Fatal(err)
	}
	setServerRegionConfig(serverConfig)
}
