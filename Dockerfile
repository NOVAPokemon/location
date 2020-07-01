FROM novapokemon/nova-server-base:latest

ENV executable="executable"
COPY $executable .
COPY configs.json .
COPY wildPokemons.json .
COPY default_server_locations.json .

CMD ["sh", "-c", "./$executable"]