package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/geo/s2"

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
	cm                  *CellManager
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
					log.Warn(WrapInit(err))
				}
			}
			log.Warn(WrapInit(err))
		} else {
			gyms, err := locationdb.GetGyms()
			if err != nil {
				log.Warn(err)
				continue
			}

			log.Infof("Loaded config: %+v", serverConfig)

			cells := convertStringsToCellIds(serverConfig.CellIdsStrings)
			cm = NewCellManager(gyms, config, cells)

			go cm.generateWildPokemonsForServerPeriodically()
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

	var boundaryConfigs []utils.LocationServerCells
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
		if err := cm.SetGyms(gyms); err != nil {
			log.Error(err)
			continue
		}
	}
}

func RefreshBoundariesPeriodic() {
	for {
		time.Sleep(time.Duration(config.UpdateConfigsInterval) * time.Second)
		serverConfig, err := locationdb.GetServerConfig(serverName)
		if err != nil {
			log.Error(err)
		} else {
			cells := convertStringsToCellIds(serverConfig.CellIdsStrings)
			cm.SetServerCells(cells)
		}
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

	err = cm.AddGym(gym)
	if err != nil {
		log.Warn("add gym to db out of my bounds")
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

func HandleSetServerConfigs(w http.ResponseWriter, r *http.Request) {
	config := &utils.LocationServerCells{}
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

	cellIds := convertStringsToCellIds(config.CellIdsStrings)
	cm.SetServerCells(cellIds)
}

// Debug method to check if world is well subdivided
func HandleGetActiveCells(w http.ResponseWriter, r *http.Request) {
	queryServerName := r.FormValue(api.GetActiveTilesRoute)
	toSend := make(map[s2.CellID]int32)
	if queryServerName == "*" {
		serverConfigs, err := locationdb.GetAllServerConfigs()
		if err != nil {
			panic(err)
		}
		for serverName := range serverConfigs {
			u := url.URL{Scheme: "http", Host: fmt.Sprintf("%s.%s", serverName, serviceNameHeadless), Path: fmt.Sprintf(api.GetServerForLocationPath)}
			q := u.Query()
			q.Set(api.ServerNamePathVar, serverName)
			resp, err := http.Get(u.String())
			if err != nil {
				panic(err)
			}
			respDecoded := map[s2.CellID]int32{}
			err = json.NewDecoder(resp.Body).Decode(&respDecoded)
			if err != nil {
				panic(err)
			}
			for cellNr, trainerNr := range respDecoded {
				toSend[cellNr] = trainerNr
			}
		}
	}

	if queryServerName == serverName {
		toSend := make(map[s2.CellID]ActiveCell)
		cm.activeCells.Range(func(cellId, activeCellValue interface{}) bool {
			activeCell := activeCellValue.(activeCellsValueType)
			toSend[cellId.(s2.CellID)] = activeCell
			return true
		})
	}

	if toWrite, err := json.Marshal(toSend); err == nil {
		_, _ = w.Write(toWrite)
	}
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

	latlon := s2.LatLngFromDegrees(lat, lon)
	cell := s2.CellIDFromLatLng(latlon)
	servers, err := getServersForCells(cell)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapGetServerForLocation(err), http.StatusInternalServerError)
		return
	}

	log.Infof("Request to determine location for Lat: %s Lon: %s | answered: %v", latStr, lonStr, servers)

	if len(servers) == 0 {
		utils.LogAndSendHTTPError(&w, wrapGetServerForLocation(
			errors.New("no server found for supplied location")), http.StatusNotFound)
		return
	}

	if len(servers) != 1 {
		utils.LogAndSendHTTPError(&w, wrapGetServerForLocation(errors.New("too many servers for location")),
			http.StatusInternalServerError)
		return
	}

	for serverAddr := range servers {
		toSend, err := json.Marshal(serverAddr)
		if err != nil {
			utils.LogAndSendHTTPError(&w, wrapGetServerForLocation(err), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(toSend)
		return
	}
}

func handleUserLocationUpdates(user string, conn *websocket.Conn) {
	inChan := make(chan *ws.Message)
	outChan := make(chan ws.GenericMsg)
	finish := make(chan struct{})

	clientChannels.Store(user, outChan)

	_ = conn.SetReadDeadline(time.Now().Add(timeoutInDuration))
	conn.SetPongHandler(func(string) error {
		// log.Warn("Received pong")
		_ = conn.SetReadDeadline(time.Now().Add(timeoutInDuration))
		return nil
	})
	doneReceive := handleMessagesLoop(conn, inChan, finish)
	doneSend := handleWriteLoop(conn, outChan, finish)
	defer func() {
		if err := cm.RemoveTrainerLocation(user); err != nil {
			log.Error(err)
		}
		close(finish)

		if err := conn.Close(); err != nil {
			log.Error(err)
		}

		<-doneReceive
		<-doneSend

		close(outChan)
		close(inChan)

		clientChannels.Delete(user)
		atomic.AddInt64(cm.totalNrTrainers, -1)
	}()
	atomic.AddInt64(cm.totalNrTrainers, 1)
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
			// log.Warn("Pinging")
			select {
			case <-finish:
			case outChan <- ws.GenericMsg{MsgType: websocket.PingMessage, Data: []byte{}}:
			}
		case <-finish:
			log.Infof("Stopped tracking user %s location", user)
			return
		}
	}
}

func handleWriteLoop(conn *websocket.Conn, channel chan ws.GenericMsg, finished chan struct{}) (done chan struct{}) {
	done = make(chan struct{})

	go func() {
		for {
			select {
			case msg, ok := <-channel:
				if !ok {
					return
				}
				// log.Info("Sending ", msg.MsgType, string(msg.Data))
				if err := conn.WriteMessage(msg.MsgType, msg.Data); err != nil {
					log.Error(ws.WrapWritingMessageError(err))
				}
			case <-finished:
				return
			}
		}
	}()

	return done
}

func handleMessagesLoop(conn *websocket.Conn, channel chan *ws.Message, finished chan struct{}) (done chan struct{}) {
	done = make(chan struct{})

	go func() {
		defer close(done)
		for {
			msg, err := clients.Read(conn)
			if err != nil {
				log.Error(err)
				return
			} else {
				select {
				case channel <- msg:
				case <-finished:
					return
				}
			}
		}
	}()

	return done
}

func handleLocationMsg(user string, msg *ws.Message) error {
	channelGeneric, ok := clientChannels.Load(user)
	if !ok {
		return wrapHandleLocationMsgs(errors.New("user not registered in this server"))
	}
	channel := channelGeneric.(valueType)

	switch msg.MsgType {
	case location.CatchPokemon:
		return handleCatchPokemonMsg(user, msg, channel)
	case location.UpdateLocation:
		// TODO remove logs
		return handleUpdateLocationMsg(user, msg, channel)
	case location.UpdateLocationWithTiles:
		return handleUpdateLocationWithTilesMsg(user, msg, channel)
	default:
		return wrapHandleLocationMsgs(ws.ErrorInvalidMessageType)
	}
}

func handleCatchPokemonMsg(user string, msg *ws.Message, channel chan ws.GenericMsg) error {
	desMsg, err := location.DeserializeLocationMsg(msg)
	if err != nil {
		msgBytes := []byte(location.CatchWildPokemonMessageResponse{
			Error: wrapCatchWildPokemonError(err).Error(),
		}.SerializeToWSMessage().Serialize())

		channel <- ws.GenericMsg{
			MsgType: websocket.TextMessage,
			Data:    msgBytes,
		}
		return wrapCatchWildPokemonError(err)
	}

	catchPokemonMsg := desMsg.(*location.CatchWildPokemonMessage)
	pokeball := catchPokemonMsg.Pokeball
	if !pokeball.IsPokeBall() {
		msgBytes := []byte(location.CatchWildPokemonMessageResponse{
			Error: wrapCatchWildPokemonError(errorInvalidItemCatch).Error(),
		}.SerializeToWSMessage().Serialize())

		channel <- ws.GenericMsg{
			MsgType: websocket.TextMessage,
			Data:    msgBytes,
		}
		return nil
	}

	wildPokemon := catchPokemonMsg.WildPokemon
	pokemonCell := s2.CellFromLatLng(wildPokemon.Location)

	cm.cellsOwnedLock.RLock()
	if !cm.cellsOwned.ContainsCell(pokemonCell) {
		cm.cellsOwnedLock.RUnlock()
		msgBytes := []byte(location.CatchWildPokemonMessageResponse{
			Error: wrapCatchWildPokemonError(newPokemonNotFoundError(wildPokemon.Pokemon.Id.Hex())).Error(),
		}.SerializeToWSMessage().Serialize())

		channel <- ws.GenericMsg{
			MsgType: websocket.TextMessage,
			Data:    msgBytes,
		}
		return wrapCatchWildPokemonError(err)
	}
	cm.cellsOwnedLock.RUnlock()

	toCatch, err := cm.GetPokemon(pokemonCell, catchPokemonMsg.WildPokemon)
	if err != nil {
		msgBytes := []byte(location.CatchWildPokemonMessageResponse{
			Error: wrapCatchWildPokemonError(err).Error(),
		}.SerializeToWSMessage().Serialize())

		channel <- ws.GenericMsg{
			MsgType: websocket.TextMessage,
			Data:    msgBytes,
		}
		return nil
	}
	pokemon := toCatch.Pokemon
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
		_, err = trainersClient.AddPokemonToTrainer(user, pokemon)
		if err != nil {

			msgBytes := []byte(location.CatchWildPokemonMessageResponse{
				Error: wrapHandleLocationMsgs(err).Error(),
			}.SerializeToWSMessage().Serialize())

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
		Error:         "",
	}.SerializeToWSMessage().Serialize())

	channel <- ws.GenericMsg{
		MsgType: websocket.TextMessage,
		Data:    msgBytes,
	}
	return nil
}

func handleUpdateLocationMsg(user string, msg *ws.Message, channel chan<- ws.GenericMsg) error {
	desMsg, err := location.DeserializeLocationMsg(msg)
	if err != nil {
		return wrapHandleLocationMsgs(err)
	}

	locationMsg := desMsg.(*location.UpdateLocationMessage)

	log.Infof("received location update from %s at location: %+v", user, locationMsg.Location)

	currCells, changed, err := cm.UpdateTrainerTiles(user, locationMsg.Location)
	if err != nil {
		return wrapHandleLocationMsgs(err)
	}

	log.Infof("User %s is on cells: %+v", user, currCells)

	cellsPerServer, err := getServersForCells(currCells...)
	if err != nil {
		return wrapHandleLocationMsgs(err)
	}

	if changed {
		var serverNames []string
		log.Infof("User %s got cellsPerServer: %+v", user, serverNames)

		for serverName := range cellsPerServer {
			serverNames = append(serverNames, serverName)
		}

		channel <- ws.GenericMsg{
			MsgType: websocket.TextMessage,
			Data: []byte(location.ServersMessage{
				Servers: serverNames,
			}.SerializeToWSMessage().Serialize()),
		}
	}

	myServer := fmt.Sprintf("%s.%s", serverName, serviceNameHeadless)
	channel <- ws.GenericMsg{
		MsgType: websocket.TextMessage,
		Data: []byte(location.CellsPerServerMessage{
			CellsPerServer: cellsPerServer,
			OriginServer:   myServer,
		}.SerializeToWSMessage().Serialize()),
	}

	err = answerToLocationMsg(channel, cellsPerServer, myServer)
	if err != nil {
		return wrapHandleLocationMsgs(err)
	}

	return nil
}

func handleUpdateLocationWithTilesMsg(user string, msg *ws.Message, channel chan<- ws.GenericMsg) error {
	desMsg, err := location.DeserializeLocationMsg(msg)
	if err != nil {
		return wrapHandleLocationMsgs(err)
	}

	locationMsg := desMsg.(*location.UpdateLocationWithTilesMessage)
	myServer := fmt.Sprintf("%s.%s", serverName, serviceNameHeadless)

	log.Infof("received precomputed location update from %s with %v\n", user, locationMsg.CellsPerServer)

	return wrapHandleLocationWithTilesMsgs(answerToLocationMsg(channel, locationMsg.CellsPerServer, myServer))
}

func answerToLocationMsg(channel chan<- ws.GenericMsg, cellsPerServer map[string]s2.CellUnion, myServer string) error {
	cells := cellsPerServer[myServer]

	if len(cells) == 0 {
		return errors.New("user contacted server that isnt responsible for any tile")
	}

	gymsInVicinity := cm.getGymsInCells(cells)
	pokemonInVicinity := cm.getPokemonsInCells(cells)

	log.Info("Sending reply to channel")
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
	log.Info("done")

	return nil
}

func getServersForCells(cells ...s2.CellID) (map[string]s2.CellUnion, error) {
	configs, err := locationdb.GetAllServerConfigs() // TODO Can be optimized, instead of fetching all the configs and looping
	if err != nil {
		return nil, err
	}

	servers := map[string]s2.CellUnion{}
	for serverName, config := range configs {
		cellIds := convertStringsToCellIds(config.CellIdsStrings)
		for _, cellID := range cells {
			if cellIds.ContainsCellID(cellID) {
				serverAddr := fmt.Sprintf("%s.%s", serverName, serviceNameHeadless)
				servers[serverAddr] = append(servers[serverAddr], cellID)
			}
		}
	}
	return servers, nil
}

func HandleForceLoadConfig(_ http.ResponseWriter, _ *http.Request) {
	serverConfig, err := locationdb.GetServerConfig(serverName)
	if err != nil {
		log.Fatal(err)
	}
	cellIds := convertStringsToCellIds(serverConfig.CellIdsStrings)
	cm.SetServerCells(cellIds)
}
