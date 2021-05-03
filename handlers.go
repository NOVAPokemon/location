package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	http "github.com/bruno-anjos/archimedesHTTPClient"
	"github.com/mitchellh/mapstructure"

	"github.com/golang/geo/s2"

	originalHTTP "net/http"

	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
	"github.com/NOVAPokemon/utils/clients"
	locationdb "github.com/NOVAPokemon/utils/database/location"
	"github.com/NOVAPokemon/utils/tokens"
	ws "github.com/NOVAPokemon/utils/websockets"
	"github.com/NOVAPokemon/utils/websockets/location"
	cedUtils "github.com/bruno-anjos/cloud-edge-deployment/pkg/utils"
	"github.com/docker/go-connections/nat"
	"github.com/golang/geo/s1"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

type (
	valueType = chan *ws.WebsocketMsg

	serverConfigsKeyType   = string
	serverConfigsValueType = *utils.LocationServerCells
)

const (
	maxCatchingProbability = 100
)

var (
	cm *cellManager

	serverName string

	timeoutInDuration = time.Duration(config.Timeout) * time.Second
	httpClient        = &http.Client{
		Client: originalHTTP.Client{
			Timeout:   clients.RequestTimeout,
			Transport: clients.NewTransport(),
		},
	}
	clientChannels = sync.Map{}
	commsManager   ws.CommunicationManager

	externalAddr string

	serverConfigs = sync.Map{}
	myLocation    s2.CellID
)

func init() {
	var exists bool
	if serverName, exists = os.LookupEnv(cedUtils.NodeIDEnvVarName); !exists {
		log.Fatal(wrapInit(errors.New("could not load server name")))
	}

	var locationToken string
	if locationToken, exists = os.LookupEnv(cedUtils.LocationEnvVarName); !exists {
		log.Panic("no location env var")
	}

	myLocation = s2.CellIDFromToken(locationToken)

	var nodeIP string
	if nodeIP, exists = os.LookupEnv(cedUtils.NodeIPEnvVarName); !exists {
		log.Panic("no ip env var")
	}

	servicePortString, err := nat.NewPort("tcp", strconv.Itoa(utils.LocationPort))
	if err != nil {
		log.Panic(err)
	}

	portMapping := cedUtils.GetPortMapFromEnvVar()
	externalPortString := portMapping[servicePortString][0].HostPort

	externalAddr = fmt.Sprintf("%s:%d", nodeIP, nat.Port(externalPortString).Int())

	log.Infof("Location: %s", locationToken)
	log.Info("Server name: ", serverName)
}

func initHandlers() {
	recalculateCells()

	log.SetLevel(log.DebugLevel)

	for i := 0; i < 5; i++ {
		time.Sleep(time.Duration(5*i) * time.Second)
		serverConfig, err := locationdb.GetServerConfig(serverName)
		if err != nil {
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

			refreshBoundaries()

			return
		}
	}
	log.Panic(wrapInit(errors.New("could not load configs from DB")))
}

func recalculateCells() {
	configs, err := locationdb.GetAllServerConfigs()
	if err != nil {
		log.Panic(err)
	}

	var myCells []string

	for currServerName, serverConfig := range configs {
		added := map[string]struct{}{}

		log.Infof("Server %s had cells %+v", currServerName, serverConfig.CellIdsStrings)

		for _, cellToken := range serverConfig.CellIdsStrings {
			cellID := s2.CellIDFromToken(cellToken)
			myDist := cellID.LatLng().Distance(myLocation.LatLng())
			otherDist := cellID.LatLng().Distance(s2.CellIDFromToken(serverConfig.Location).LatLng())

			if myDist < otherDist {
				myCells = append(myCells, cellToken)
				added[cellToken] = struct{}{}
			}
		}

		if len(added) > 0 {
			var newServerConfig []string

			for _, cellToken := range serverConfig.CellIdsStrings {
				if _, ok := added[cellToken]; ok {
					continue
				}
				newServerConfig = append(newServerConfig, cellToken)
			}

			log.Infof("Server %s has new config %+v", currServerName, newServerConfig)

			serverConfig.CellIdsStrings = newServerConfig
			err = locationdb.UpdateServerConfig(currServerName, serverConfig)
			if err != nil {
				log.Error(err)
			}
		}
	}

	myConfig := utils.LocationServerCells{
		CellIdsStrings: myCells,
		Location:       myLocation.ToToken(),
		Addr:           externalAddr,
		ServerName:     serverName,
	}

	log.Infof("I have cells %+v", myConfig.CellIdsStrings)

	err = locationdb.UpdateServerConfig(serverName, myConfig)
	if err != nil {
		log.Panic(err)
	}
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

func refreshBoundaries() {
	log.Debug("refreshing boundaries")

	myServerConfig, err := locationdb.GetServerConfig(serverName)
	if err != nil {
		log.Error(err)
	} else {
		cells := convertCellTokensToIds(myServerConfig.CellIdsStrings)
		cm.setServerCells(cells)
	}

	configs, err := locationdb.GetAllServerConfigs()
	if err != nil {
		log.Panic(err)
	}

	for currServerName, serverConfig := range configs {
		serverConfigCopy := serverConfig
		serverConfigs.Store(currServerName, &serverConfigCopy)
	}

	auxServerConfigs := map[serverConfigsKeyType]serverConfigsValueType{}
	serverConfigs.Range(func(key, value interface{}) bool {
		typedKey := key.(serverConfigsKeyType)
		typedValue := value.(serverConfigsValueType)

		auxServerConfigs[typedKey] = typedValue

		return true
	})

	log.Debugf("boundaries: %+v", auxServerConfigs)
}

func refreshBoundariesPeriodic() {
	for {
		refreshBoundaries()
		time.Sleep(time.Duration(config.UpdateConfigsInterval) * time.Second)
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
		configs, fetchErr := locationdb.GetAllServerConfigs()
		if fetchErr != nil {
			panic(fetchErr)
		}
		wg := &sync.WaitGroup{}
		for currServerName := range configs {
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
				resolvedAddr, _, err := httpClient.ResolveServiceInArchimedes(fmt.Sprintf("%s:%d", serverAddr, port))
				if err != nil {
					log.Panic(err)
				}

				u := url.URL{Scheme: "http", Host: resolvedAddr, Path: fmt.Sprintf(api.GetActiveCells, serverAddr)}
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
		resolvedAddr, _, err := httpClient.ResolveServiceInArchimedes(fmt.Sprintf("%s:%d", queryServerName, port))
		if err != nil {
			log.Panic(err)
		}

		u := url.URL{Scheme: "http", Host: resolvedAddr, Path: fmt.Sprintf(api.GetActiveCells, queryServerName)}
		var resp *http.Response
		resp, err = http.Get(u.String())
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
		configs, fetchErr := locationdb.GetAllServerConfigs()
		if fetchErr != nil {
			panic(fetchErr)
		}
		wg := &sync.WaitGroup{}
		for currServerName := range configs {
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
				resolvedAddr, _, err := httpClient.ResolveServiceInArchimedes(fmt.Sprintf("%s:%d", serverAddr, port))
				if err != nil {
					log.Panic(err)
				}

				u := url.URL{Scheme: "http", Host: resolvedAddr, Path: fmt.Sprintf(api.GetActivePokemons, serverAddr)}
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
		resolvedAddr, _, err := httpClient.ResolveServiceInArchimedes(fmt.Sprintf("%s:%d", queryServerName, port))
		if err != nil {
			log.Panic(err)
		}

		u := url.URL{Scheme: "http", Host: resolvedAddr, Path: fmt.Sprintf(api.GetActivePokemons, queryServerName)}
		var resp *http.Response
		resp, err = http.Get(u.String())
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

	configs, err := locationdb.GetAllServerConfigs()
	if err != nil {
		log.Fatal(err)
	}
	serverConfigsWithBounds := make([]regionConfig, 0, len(configs))
	for _, serverCfg := range configs {
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
		log.Infof("will finish connection to %s", user)

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
				log.Warn(ws.WrapWritingMessageError(err))
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(timeoutInDuration))
		case <-pingTicker.C:
			select {
			case <-finish:
			case outChan <- ws.NewControlMsg(websocket.PingMessage):
			}
		case <-doneSend:
			log.Infof("disconnected user %s", user)
			return
		}
	}
}

func handleGetServerForLocation(w http.ResponseWriter, r *http.Request) {
	log.Info("handling get server for location request")

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

		var serverConfig *utils.LocationServerCells
		serverConfig, err = locationdb.GetServerConfig(serverName)
		if err != nil {
			log.Panic(err)
		}

		serverConfig.CellIdsStrings = append(serverConfig.CellIdsStrings, cell.ToToken())
		err = locationdb.UpdateServerConfig(serverName, *serverConfig)
		if err != nil {
			log.Panic(err)
		}

		_, _ = w.Write(toSend)
		return
	}
}

func handleWriteLoop(conn *websocket.Conn, channel <-chan *ws.WebsocketMsg, finished chan struct{},
	writer ws.CommunicationManager) (done chan struct{}) {
	done = make(chan struct{})

	go func() {
		defer close(done)
		for {
			select {
			case msg, ok := <-channel:
				if !ok {
					return
				}

				err := writer.WriteGenericMessageToConn(conn, msg)
				if err != nil {
					log.Warn(ws.WrapWritingMessageError(err))
					return
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
		trainersClient := clients.NewTrainersClient(httpClient, commsManager)
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

	currCells, changed, disconnect, err := cm.updateTrainerTiles(user, locationMsg.Location)
	if err != nil {
		return wrapHandleLocationMsgs(err)
	}

	if disconnect {
		log.Infof("telling %s to disconnect", user)
		channel <- location.DisconnectMessage{Addr: externalAddr}.ConvertToWSMessage()
		return nil
	}

	currCellsPrint := make([]string, len(currCells))
	for i, cellID := range currCells {
		currCellsPrint[i] = cellID.ToToken()
	}

	log.Infof("User %s is on %d cells: %+v", user, len(currCells), currCellsPrint)

	cellsPerServer, err := getServersForCells(currCells...)
	if err != nil {
		return wrapHandleLocationMsgs(err)
	}

	log.Infof("User %s got cellsPerServer: %+v", user, cellsPerServer)

	if changed {
		var serverNames []string

		for serverNameAux := range cellsPerServer {
			serverNames = append(serverNames, serverNameAux)
		}

		channel <- location.ServersMessage{
			Servers: serverNames,
		}.ConvertToWSMessage(info)
	}

	channel <- location.CellsPerServerMessage{
		CellsPerServer: cellsPerServer,
		OriginServer:   externalAddr,
	}.ConvertToWSMessage(info)

	err = answerToLocationMsg(channel, info, cellsPerServer, user)
	if err != nil {
		log.Warn(wrapHandleLocationMsgs(err))
	}

	return nil
}

func getServersForCells(cells ...s2.CellID) (servers map[string]s2.CellUnion, err error) {
	servers = map[string]s2.CellUnion{}

	for _, cellID := range cells {
		var (
			minDist       s1.Angle
			minServerAddr string
			notSet        = true
		)

		serverConfigs.Range(func(key, value interface{}) bool {
			serverConfig := value.(serverConfigsValueType)

			dist := cellID.LatLng().Distance(s2.CellIDFromToken(serverConfig.Location).LatLng())
			if dist < minDist || notSet {
				minDist = dist
				minServerAddr = serverConfig.Addr
				notSet = false
			}

			return true
		})

		servers[minServerAddr] = append(servers[minServerAddr], cellID)
	}

	return servers, nil
}

func handleUpdateLocationWithTilesMsg(user string, ulMsg *location.UpdateLocationWithTilesMessage, info ws.TrackedInfo,
	channel chan<- *ws.WebsocketMsg) error {
	log.Infof("received precomputed location update from %s with %v\n", user, ulMsg.CellsPerServer)

	return wrapHandleLocationWithTilesMsgs(answerToLocationMsg(channel, info, ulMsg.CellsPerServer, user))
}

func answerToLocationMsg(channel chan<- *ws.WebsocketMsg, info ws.TrackedInfo,
	cellsPerServer map[string]s2.CellUnion, user string) error {
	cells := cellsPerServer[externalAddr]

	if len(cells) == 0 {
		log.Infof("(%s) me %s %+v", user, externalAddr, cellsPerServer)
		return errors.New(fmt.Sprintf("user %s contacted server that isnt responsible for any tile", user))
	}

	gymsInVicinity := cm.getGymsInCells(cells)
	pokemonInVicinity := cm.getPokemonsInCells(cells)

	gymsMsg := location.GymsMessage{Gyms: gymsInVicinity}.ConvertToWSMessage(info)
	pokemonsMsg := location.PokemonMessage{
		Pokemon: pokemonInVicinity,
	}.ConvertToWSMessage(info)

	log.Infof("Sending reply to channel %+v, %+v", gymsMsg, pokemonsMsg)

	channel <- gymsMsg

	channel <- pokemonsMsg
	log.Info("done")

	return nil
}

func handleForceLoadConfig(_ http.ResponseWriter, _ *http.Request) {
	serverConfig, err := locationdb.GetServerConfig(serverName)
	if err != nil {
		log.Fatal(err)
	}
	cellIds := convertCellTokensToIds(serverConfig.CellIdsStrings)
	cm.setServerCells(cellIds)
}

func handleBeingRemoved(_ http.ResponseWriter, _ *http.Request) {
	configs, err := locationdb.GetAllServerConfigs()
	if err != nil {
		log.Panic(err)
	}

	myCells := configs[serverName].CellIdsStrings
	delete(configs, serverName)

	changed := map[string][]string{}

	for _, cellToken := range myCells {
		var (
			minDist   s1.Angle
			minServer string
			notSet    = true
		)

		cellID := s2.CellIDFromToken(cellToken)

		for _, serverConfig := range configs {
			dist := cellID.LatLng().Distance(s2.CellIDFromToken(serverConfig.Location).LatLng())
			if dist < minDist || notSet {
				minDist = dist
				minServer = serverConfig.ServerName
				notSet = false
			}
		}

		changed[minServer] = append(changed[minServer], cellToken)
	}

	for server, cells := range changed {
		configAux := configs[serverName]
		configAux.CellIdsStrings = cells
		err = locationdb.UpdateServerConfig(server, configAux)
		if err != nil {
			log.Panic(err)
		}
	}
}

func calculcateClosestServerToLocation(location s2.LatLng) string {
	var (
		closest string
		minDist s1.Angle
		unset   = true
	)

	serverConfigs.Range(func(key, value interface{}) bool {
		name := key.(serverConfigsKeyType)
		configAux := value.(serverConfigsValueType)

		dist := s2.CellIDFromToken(configAux.Location).LatLng().Distance(location)
		if unset || dist < minDist {
			minDist = dist
			closest = name
			unset = false
		}

		return true
	})

	return closest
}
