module github.com/NOVAPokemon/location

go 1.13

require (
	github.com/NOVAPokemon/utils v0.0.62
	github.com/golang/geo v0.0.0-20200319012246-673a6f80352d
	github.com/gorilla/mux v1.7.4
	github.com/gorilla/websocket v1.4.2
	github.com/pkg/errors v0.8.1
	github.com/prometheus/client_golang v1.6.0
	github.com/sirupsen/logrus v1.5.0
	github.com/stretchr/testify v1.4.0
)

replace github.com/NOVAPokemon/utils v0.0.62 => ../utils
