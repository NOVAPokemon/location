module github.com/NOVAPokemon/location

go 1.13

require (
	github.com/NOVAPokemon/client v0.0.0-20200407183259-62f2a750d37d // indirect
	github.com/NOVAPokemon/utils v0.0.62
	github.com/sirupsen/logrus v1.5.0
	go.mongodb.org/mongo-driver v1.3.1
)

replace github.com/NOVAPokemon/utils v0.0.62 => ../utils
