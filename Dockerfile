FROM nova-server-base:latest

ENV executable="executable"
COPY $executable .
COPY configs.json .
COPY pokemons.json .

CMD ["sh", "-c", "./$executable"]