FROM golang:latest

ENV executable="executable"

RUN mkdir /service
WORKDIR /service
COPY $executable .
COPY configs.json .
COPY example_gyms.json .
COPY pokemons.json .

ENTRYPOINT ./$executable