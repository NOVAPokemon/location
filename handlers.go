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

	"github.com/mitchellh/mapstructure"

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
	valueType = chan *ws.WebsocketMsg
)

const (
	maxCatchingProbability = 100
)

var (
	cm                  *cellManager
	serverName          string
	serverNr            int64
	timeoutInDuration   = time.Duration(config.Timeout) * time.Second
	httpClient          = &http.Client{Timeout: clients.RequestTimeout}
	basicClient         = clients.NewBasicClient(false, "")
	clientChannels      = sync.Map{}
	serviceNameHeadless string
	commsManager        ws.CommunicationManager
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
		log.Fatal(wrapInit(errors.New("could not load server name")))
	}
	log.Info("Server name: ", serverName)

	split := strings.Split(serverName, "-")
	if serverNrTmp, err := strconv.ParseInt(split[len(split)-1], 10, 32); err != nil {
		log.Fatal(wrapInit(err))
	} else {
		serverNr = serverNrTmp
	}
}

func initHandlers() {
	for i := 0; i < 5; i++ {
		time.Sleep(time.Duration(5*i) * time.Second)
		serverConfig, err := locationdb.GetServerConfig(serverName)
		if err != nil {
			if serverNr == 0 {
				// if configs are missing, server 0 adds them
				err = insertDefaultBoundariesInDB()
				if err != nil {
					log.Warn(wrapInit(err))
				}
			} else {
				log.Warn(wrapInit(err))
			}
		} else {
			var gyms []utils.GymWithServer
			gyms, err = locationdb.GetGyms()
			if err != nil {
				log.Warn(err)
				continue
			}

			log.Infof("Loaded config: %+v", serverConfig)

			cells := convertCellTokensToIds(serverConfig.CellIdsStrings)
			cm = newCellManager(gyms, config, cells)

			go cm.generateWildPokemonsForServerPeriodically()
			go refreshBoundariesPeriodic()
			go refreshGymsPeriodic()
			return
		}
	}
	log.Panic(wrapInit(errors.New("could not load configs from DB")))
}

func insertDefaultBoundariesInDB() error {
	data, err := ioutil.ReadFile(defaultServerBoundariesFile)
	if err != nil {
		return wrapLoadServerBoundaries(err)
	}

	var boundaryConfigs []utils.LocationServerCells
	err = json.Unmarshal(data, &boundaryConfigs)
	if err != nil {
		return wrapLoadServerBoundaries(err)
	}

	for i := 0; i < len(boundaryConfigs); i++ {
		if err = locationdb.UpdateServerConfig(boundaryConfigs[i].ServerName, boundaryConfigs[i]); err != nil {
			log.Warn(wrapLoadServerBoundaries(err))
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
		if err = cm.setGyms(gyms); err != nil {
			log.Error(err)
			continue
		}
	}
}

func refreshBoundariesPeriodic() {
	for {
		time.Sleep(time.Duration(config.UpdateConfigsInterval) * time.Second)
		serverConfig, err := locationdb.GetServerConfig(serverName)
		if err != nil {
			log.Error(err)
		} else {
			cells := convertCellTokensToIds(serverConfig.CellIdsStrings)
			cm.setServerCells(cells)
		}
	}
}

func handleAddGymLocation(w http.ResponseWriter, r *http.Request) {
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

	err = cm.addGym(gym)
	if err != nil {
		log.Warn("add gym to db out of my bounds")
		return
	}
}

func handleUserLocation(w http.ResponseWriter, r *http.Request) {
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

func handleSetServerConfigs(w http.ResponseWriter, r *http.Request) {
	configAux := &utils.LocationServerCells{}
	servername := mux.Vars(r)[api.ServerNamePathVar]
	err := json.NewDecoder(r.Body).Decode(configAux)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapSetServerConfigsError(err), http.StatusBadRequest)
		return
	}
	err = locationdb.UpdateServerConfig(servername, *configAux)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapSetServerConfigsError(err), http.StatusInternalServerError)
		return
	}
	cellIds := convertCellTokensToIds(configAux.CellIdsStrings)
	cm.setServerCells(cellIds)
}

// HandleGetActiveCells is a debug method to check if world is well subdivided
func handleGetActiveCells(w http.ResponseWriter, r *http.Request) {
	type trainersInCell struct {
		CellID     string      `json:"cell_id"`
		TrainersNr int64       `json:"trainers_nr"`
		CellBounds [][]float64 `json:"cell_bounds"`
	}

	tmpMap := sync.Map{}
	queryServerName := mux.Vars(r)[api.ServerNamePathVar]
	log.Info("Request to get active cells")
	if queryServerName == "all" {
		log.Info("Getting active cells for all servers")
		serverConfigs, fetchErr := locationdb.GetAllServerConfigs()
		if fetchErr != nil {
			panic(fetchErr)
		}
		wg := &sync.WaitGroup{}
		for currServerName := range serverConfigs {
			serverAddr := currServerName

			if serverAddr == serverName {
				cm.activeCells.Range(func(cellId, activeCellValue interface{}) bool {
					cell := activeCellValue.(activeCellsValueType)
					tmpMap.Store(cellId.(s2.CellID).ToToken(), cell.getNrTrainers())
					return true
				})
				continue
			}

			wg.Add(1)
			go func() {
				u := url.URL{Scheme: "http", Host: fmt.Sprintf("%s.%s:%d", serverAddr, serviceNameHeadless, port), Path: fmt.Sprintf(api.GetActiveCells, serverAddr)}
				resp, err := http.Get(u.String())
				if err != nil {
					panic(err)
				}
				var respDecoded []trainersInCell
				err = json.NewDecoder(resp.Body).Decode(&respDecoded)
				if err != nil {
					panic(err)
				}
				for _, curr := range respDecoded {
					tmpMap.Store(curr.CellID, curr.TrainersNr)
				}
				wg.Done()
				log.Infof("Done getting active cells from server %s", serverAddr)
			}()
		}
		wg.Wait()
	} else if serverName == queryServerName {
		log.Info("Responding with active cells...")
		cm.activeCells.Range(func(cellId, activeCellValue interface{}) bool {
			cell := activeCellValue.(activeCellsValueType)
			tmpMap.Store(cellId.(s2.CellID).ToToken(), cell.getNrTrainers())
			return true
		})
	} else {
		u := url.URL{Scheme: "http", Host: fmt.Sprintf("%s.%s:%d", queryServerName, serviceNameHeadless, port), Path: fmt.Sprintf(api.GetActiveCells, queryServerName)}
		var resp *http.Response
		resp, err := http.Get(u.String())
		if err != nil {
			log.Error(err)
		}
		var respDecoded []trainersInCell
		err = json.NewDecoder(resp.Body).Decode(&respDecoded)
		if err != nil {
			log.Error(err)
		}
		for _, curr := range respDecoded {
			tmpMap.Store(curr.CellID, curr.TrainersNr)
		}
	}

	var toSend []trainersInCell
	tmpMap.Range(func(cellID, trainersNr interface{}) bool {
		cellRect := s2.CellFromCellID(s2.CellIDFromToken(cellID.(string))).RectBound()
		points := [][]float64{
			{cellRect.Vertex(0).Lat.Degrees(), cellRect.Vertex(0).Lng.Degrees()},
			{cellRect.Vertex(1).Lat.Degrees(), cellRect.Vertex(1).Lng.Degrees()},
			{cellRect.Vertex(2).Lat.Degrees(), cellRect.Vertex(2).Lng.Degrees()},
			{cellRect.Vertex(3).Lat.Degrees(), cellRect.Vertex(3).Lng.Degrees()},
		}
		toAppend := trainersInCell{
			CellID:     cellID.(string),
			TrainersNr: trainersNr.(int64),
			CellBounds: points,
		}
		toSend = append(toSend, toAppend)
		return true
	})

	toWrite, err := json.Marshal(toSend)
	if err != nil {
		panic(err)
	} else {
		_, _ = w.Write(toWrite)
	}
}

// HandleGetActiveCells is a debug method to check if world is well subdivided
func handleGetActivePokemons(w http.ResponseWriter, r *http.Request) {
	type activePokemon struct {
		Id     string    `json:"pokemon_id"`
		LatLng []float64 `json:"cell_bounds"`
		Server string    `json:"server"`
	}

	tmpMap := sync.Map{}
	queryServerName := mux.Vars(r)[api.ServerNamePathVar]
	log.Info("Request to get active pokemons")
	if queryServerName == "all" {
		log.Info("Getting active pokemons for all servers")
		serverConfigs, fetchErr := locationdb.GetAllServerConfigs()
		if fetchErr != nil {
			panic(fetchErr)
		}
		wg := &sync.WaitGroup{}
		for currServerName := range serverConfigs {
			serverAddr := currServerName

			if serverAddr == serverName {
				cm.activeCells.Range(func(cellId, activeCellValue interface{}) bool {
					cell := activeCellValue.(activeCellsValueType)
					pokemons := cell.wildPokemons
					pokemons.Range(func(_, value interface{}) bool {
						pokemon := value.(utils.WildPokemonWithServer)
						toAdd := activePokemon{
							Id:     pokemon.Pokemon.Id,
							LatLng: []float64{pokemon.Location.Lat.Degrees(), pokemon.Location.Lng.Degrees()},
							Server: pokemon.Server,
						}
						tmpMap.Store(toAdd.Id, toAdd)
						return true
					})
					return true
				})
				continue
			}

			wg.Add(1)
			go func() {
				u := url.URL{Scheme: "http", Host: fmt.Sprintf("%s.%s:%d", serverAddr, serviceNameHeadless, port), Path: fmt.Sprintf(api.GetActivePokemons, serverAddr)}
				resp, err := http.Get(u.String())
				if err != nil {
					panic(err)
				}
				var respDecoded []activePokemon
				err = json.NewDecoder(resp.Body).Decode(&respDecoded)
				if err != nil {
					panic(err)
				}
				for _, curr := range respDecoded {
					tmpMap.Store(curr.Id, curr)
				}
				wg.Done()
				log.Infof("Done getting active cells from server %s", serverAddr)
			}()
		}
		wg.Wait()
	} else if serverName == queryServerName {
		log.Info("Responding with active pokemons...")
		cm.activeCells.Range(func(cellId, activeCellValue interface{}) bool {
			cell := activeCellValue.(activeCellsValueType)
			pokemons := cell.wildPokemons
			pokemons.Range(func(_, value interface{}) bool {
				pokemon := value.(utils.WildPokemonWithServer)
				toAdd := activePokemon{
					Id:     pokemon.Pokemon.Id,
					LatLng: []float64{pokemon.Location.Lat.Degrees(), pokemon.Location.Lng.Degrees()},
					Server: pokemon.Server,
				}
				tmpMap.Store(toAdd.Id, toAdd)
				return true
			})
			return true
		})
	} else {
		u := url.URL{Scheme: "http", Host: fmt.Sprintf("%s.%s:%d", queryServerName, serviceNameHeadless, port), Path: fmt.Sprintf(api.GetActivePokemons, queryServerName)}
		var resp *http.Response
		resp, err := http.Get(u.String())
		if err != nil {
			log.Error(err)
		}
		var respDecoded []activePokemon
		err = json.NewDecoder(resp.Body).Decode(&respDecoded)
		if err != nil {
			log.Error(err)
		}
		for _, pokemon := range respDecoded {
			tmpMap.Store(pokemon.Id, pokemon)
		}
	}

	var toSend []activePokemon
	tmpMap.Range(func(_, pokemon interface{}) bool {
		toSend = append(toSend, pokemon.(activePokemon))
		return true
	})

	toWrite, err := json.Marshal(toSend)
	if err != nil {
		panic(err)
	} else {
		_, _ = w.Write(toWrite)
	}
}

func handleGetGlobalRegionSettings(w http.ResponseWriter, _ *http.Request) {
	type regionConfig struct {
		ServerName string
		CellIDs    []string      `json:"cell_id"`
		CellBounds [][][]float64 `json:"cell_bounds"`
	}

	serverConfigs, err := locationdb.GetAllServerConfigs()
	if err != nil {
		log.Fatal(err)
	}
	serverConfigsWithBounds := make([]regionConfig, 0, len(serverConfigs))
	for _, serverCfg := range serverConfigs {
		cellBounds := make([][][]float64, 0, len(serverCfg.CellIdsStrings))
		for _, cellToken := range serverCfg.CellIdsStrings {
			cellRect := s2.CellFromCellID(s2.CellIDFromToken(cellToken)).RectBound()
			points := [][]float64{
				{cellRect.Vertex(0).Lat.Degrees(), cellRect.Vertex(0).Lng.Degrees()},
				{cellRect.Vertex(1).Lat.Degrees(), cellRect.Vertex(1).Lng.Degrees()},
				{cellRect.Vertex(2).Lat.Degrees(), cellRect.Vertex(2).Lng.Degrees()},
				{cellRect.Vertex(3).Lat.Degrees(), cellRect.Vertex(3).Lng.Degrees()},
			}
			cellBounds = append(cellBounds, points)
		}
		serverConfigsWithBounds = append(serverConfigsWithBounds, regionConfig{
			ServerName: serverCfg.ServerName,
			CellIDs:    serverCfg.CellIdsStrings,
			CellBounds: cellBounds,
		})
	}
	toSend, err := json.Marshal(serverConfigsWithBounds)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapGetAllConfigs(err), http.StatusInternalServerError)
	}
	_, _ = w.Write(toSend)
}

func handleGetServerForLocation(w http.ResponseWriter, r *http.Request) {
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
		var toSend []byte
		toSend, err = json.Marshal(serverAddr)
		if err != nil {
			utils.LogAndSendHTTPError(&w, wrapGetServerForLocation(err), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(toSend)
		return
	}
}

func handleUserLocationUpdates(user string, conn *websocket.Conn) {
	inChan := make(chan *ws.WebsocketMsg)
	outChan := make(valueType)
	finish := make(chan struct{})

	clientChannels.Store(user, outChan)

	_ = conn.SetReadDeadline(time.Now().Add(timeoutInDuration))
	conn.SetPongHandler(func(string) error {
		// log.Warn("Received pong")
		_ = conn.SetReadDeadline(time.Now().Add(timeoutInDuration))
		return nil
	})
	doneReceive := handleMessagesLoop(conn, inChan, finish)
	doneSend := handleWriteLoop(conn, outChan, finish, commsManager)

	defer func() {
		if err := cm.removeTrainerLocation(user); err != nil {
			log.Warn(err)
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
	pingTicker := time.NewTicker(time.Duration(config.Ping) * time.Second)
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
			select {
			case <-finish:
			case outChan <- ws.NewControlMsg(websocket.PingMessage):
			}
		case <-finish:
			log.Infof("Stopped tracking user %s location", user)
			return
		}
	}
}

func handleWriteLoop(conn *websocket.Conn, channel <-chan *ws.WebsocketMsg, finished chan struct{},
	writer ws.CommunicationManager) (done chan struct{}) {
	done = make(chan struct{})

	go func() {
		for {
			select {
			case msg, ok := <-channel:
				if !ok {
					return
				}

				err := writer.WriteGenericMessageToConn(conn, msg)
				if err != nil {
					log.Error(ws.WrapWritingMessageError(err))
				}
			case <-finished:
				return
			}
		}
	}()

	return done
}

func handleMessagesLoop(conn *websocket.Conn, channel chan *ws.WebsocketMsg,
	finished chan struct{}) (done chan struct{}) {
	done = make(chan struct{})

	go func() {
		defer close(done)
		for {
			wsMsg, err := clients.Read(conn, commsManager)
			if err != nil {
				log.Warn(err)
				return
			}

			if err != nil {
				panic(err)
				return
			} else {
				select {
				case channel <- wsMsg:
				case <-finished:
					return
				}
			}
		}
	}()

	return done
}

func handleLocationMsg(user string, wsMsg *ws.WebsocketMsg) error {
	channelGeneric, ok := clientChannels.Load(user)
	if !ok {
		return wrapHandleLocationMsgs(errors.New("user not registered in this server"))
	}

	channel := channelGeneric.(valueType)

	info := *wsMsg.Content.RequestTrack
	msgData := wsMsg.Content.Data

	switch wsMsg.Content.AppMsgType {
	case location.CatchPokemon:

		catchPokemonMsg := &location.CatchWildPokemonMessage{}
		err := mapstructure.Decode(msgData, &catchPokemonMsg)
		if err != nil {
			panic(err)
		}
		return handleCatchPokemonMsg(user, catchPokemonMsg, info, channel)
	case location.UpdateLocation:
		ulMsg := &location.UpdateLocationMessage{}
		err := mapstructure.Decode(msgData, &ulMsg)
		if err != nil {
			panic(err)
		}
		return handleUpdateLocationMsg(user, ulMsg, info, channel)
	case location.UpdateLocationWithTiles:
		ulwtMsg := &location.UpdateLocationWithTilesMessage{}
		err := mapstructure.Decode(msgData, &ulwtMsg)
		if err != nil {
			panic(err)
		}
		return handleUpdateLocationWithTilesMsg(user, ulwtMsg, info, channel)
	default:
		return wrapHandleLocationMsgs(ws.ErrorInvalidMessageType)
	}
}

func handleCatchPokemonMsg(user string, catchPokemonMsg *location.CatchWildPokemonMessage, info ws.TrackedInfo,
	channel chan *ws.WebsocketMsg) error {
	pokeball := catchPokemonMsg.Pokeball
	if !pokeball.IsPokeBall() {
		channel <- location.CatchWildPokemonMessageResponse{
			Error: wrapCatchWildPokemonError(errorInvalidItemCatch).Error(),
		}.ConvertToWSMessage(info)
		return nil
	}

	wildPokemon := catchPokemonMsg.WildPokemon
	pokemonCell := s2.CellFromLatLng(wildPokemon.Location)

	cm.cellsOwnedLock.RLock()
	if !cm.cellsOwned.ContainsCell(pokemonCell) {
		cm.cellsOwnedLock.RUnlock()
		err := wrapCatchWildPokemonError(newPokemonNotFoundError(wildPokemon.Pokemon.Id))
		if err == nil {
			panic("error should not be nil")
		}

		channel <- location.CatchWildPokemonMessageResponse{
			Error: err.Error(),
		}.ConvertToWSMessage(info)

		return wrapCatchWildPokemonError(err)
	}
	cm.cellsOwnedLock.RUnlock()

	toCatch, err := cm.getPokemon(pokemonCell, catchPokemonMsg.WildPokemon)
	if err != nil {
		channel <- location.CatchWildPokemonMessageResponse{
			Error: wrapCatchWildPokemonError(err).Error(),
		}.ConvertToWSMessage(info)
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
		trainersClient := clients.NewTrainersClient(httpClient, commsManager, basicClient)
		_, err = trainersClient.AddPokemonToTrainer(user, pokemon)
		if err != nil {
			channel <- location.CatchWildPokemonMessageResponse{
				Error: wrapHandleLocationMsgs(err).Error(),
			}.ConvertToWSMessage(info)
			return err
		}
		pokemonTokens = make([]string, 0, len(trainersClient.PokemonTokens))
		for _, tokenString := range trainersClient.PokemonTokens {
			pokemonTokens = append(pokemonTokens, tokenString)
		}
	}

	channel <- location.CatchWildPokemonMessageResponse{
		Caught:        caught,
		PokemonTokens: pokemonTokens,
		Error:         "",
	}.ConvertToWSMessage(info)
	return nil
}

func handleUpdateLocationMsg(user string, locationMsg *location.UpdateLocationMessage, info ws.TrackedInfo,
	channel chan<- *ws.WebsocketMsg) error {
	log.Infof("received location update from %s at location: %+v", user, locationMsg.Location)

	currCells, changed, err := cm.updateTrainerTiles(user, locationMsg.Location)
	if err != nil {
		return wrapHandleLocationMsgs(err)
	}

	log.Infof("User %s is on %d cells: %+v", user, len(currCells), currCells)

	cellsPerServer, err := getServersForCells(currCells...)
	if err != nil {
		return wrapHandleLocationMsgs(err)
	}

	if changed {
		var serverNames []string
		log.Infof("User %s got cellsPerServer: %+v", user, serverNames)

		for serverNameAux := range cellsPerServer {
			serverNames = append(serverNames, serverNameAux)
		}

		channel <- location.ServersMessage{
			Servers: serverNames,
		}.ConvertToWSMessage(info)
	}

	myServer := fmt.Sprintf("%s.%s", serverName, serviceNameHeadless)
	channel <- location.CellsPerServerMessage{
		CellsPerServer: cellsPerServer,
		OriginServer:   myServer,
	}.ConvertToWSMessage(info)

	err = answerToLocationMsg(channel, info, cellsPerServer, myServer)
	if err != nil {
		return wrapHandleLocationMsgs(err)
	}

	return nil
}

func handleUpdateLocationWithTilesMsg(user string, ulMsg *location.UpdateLocationWithTilesMessage, info ws.TrackedInfo,
	channel chan<- *ws.WebsocketMsg) error {
	myServer := fmt.Sprintf("%s.%s", serverName, serviceNameHeadless)

	log.Infof("received precomputed location update from %s with %v\n", user, ulMsg.CellsPerServer)

	return wrapHandleLocationWithTilesMsgs(answerToLocationMsg(channel, info, ulMsg.CellsPerServer, myServer))
}

func answerToLocationMsg(channel chan<- *ws.WebsocketMsg, info ws.TrackedInfo, cellsPerServer map[string]s2.CellUnion,
	myServer string) error {
	cells := cellsPerServer[myServer]

	if len(cells) == 0 {
		return errors.New("user contacted server that isnt responsible for any tile")
	}

	gymsInVicinity := cm.getGymsInCells(cells)
	pokemonInVicinity := cm.getPokemonsInCells(cells)

	log.Info("Sending reply to channel")
	channel <- location.GymsMessage{Gyms: gymsInVicinity}.ConvertToWSMessage(info)

	channel <- location.PokemonMessage{
		Pokemon: pokemonInVicinity,
	}.ConvertToWSMessage(info)
	log.Info("done")

	return nil
}

func getServersForCells(cells ...s2.CellID) (map[string]s2.CellUnion, error) {
	// TODO Can be optimized, instead of fetching all the configs and looping
	configs, err := locationdb.GetAllServerConfigs()
	if err != nil {
		return nil, err
	}

	servers := map[string]s2.CellUnion{}

	for serverNameAux, configAux := range configs {
		cellIds := convertCellTokensToIds(configAux.CellIdsStrings)
		for _, cellID := range cells {
			if cellIds.ContainsCellID(cellID) {
				servers[serverNameAux] = append(servers[serverNameAux], cellID)
			}
		}
	}
	return servers, nil
}

func handleForceLoadConfig(_ http.ResponseWriter, _ *http.Request) {
	serverConfig, err := locationdb.GetServerConfig(serverName)
	if err != nil {
		log.Fatal(err)
	}
	cellIds := convertCellTokensToIds(serverConfig.CellIdsStrings)
	cm.setServerCells(cellIds)
}
