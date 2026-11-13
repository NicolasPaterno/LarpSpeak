# Larpspeak: atalhos para subir o ambiente, entrar em chamadas e rodar os cenários F1-F8.
# `make help` lista tudo.

# Endereço que os clientes de voz usam para alcançar esta máquina (IP da LAN).
RELAY_HOST ?= $(shell ipconfig getifaddr en0 2>/dev/null || hostname -I 2>/dev/null | awk '{print $$1}')
export RELAY_HOST

COMPOSE := docker compose -f docker-compose.yml -f compose.voice.yml
SERVER  ?= http://localhost:8081
CLIENT  := bin/voice-client

# Opções do cliente de voz: MUTE=1 DEAFEN=1 NOMIC=1 LOSS=0.1 RELAY=<host>
CLIENT_FLAGS = -server $(SERVER) -user $(ME) $(if $(MUTE),-mute) $(if $(DEAFEN),-deafen) \
	$(if $(NOMIC),-nomic) $(if $(LOSS),-loss $(LOSS)) $(if $(RELAY),-relay $(RELAY))

.PHONY: help build start stop clean logs ps call join listen invite leave status mute deafen test

help: ## lista os comandos
	@grep -E '^[a-z_%0-9-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  make %-14s %s\n", $$1, $$2}'

# --- ambiente ---

build: ## compila as imagens dos serviços e o cliente de voz (bin/voice-client)
	$(COMPOSE) build
	@mkdir -p bin
	@if command -v go >/dev/null; then cd tools/voice-client && go build -o ../../bin/voice-client .; \
	else docker run --rm -v "$$PWD/tools/voice-client:/src" -v "$$PWD/bin:/out" -w /src \
		-e CGO_ENABLED=0 -e GOOS=$$(uname -s | tr A-Z a-z) -e GOARCH=$$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') \
		golang:1.27-alpine3.23 go build -o /out/voice-client .; fi
	@echo "pronto: make start"

start: _need_host ## sobe todos os serviços (voz acessível em $RELAY_HOST)
	$(COMPOSE) up -d
	@echo "call-signaling: $(SERVER)  |  relays de voz: $(RELAY_HOST):40000 e :40001 (UDP)"

stop: ## derruba os serviços (o MiniStack guarda estado em memória: tudo zera)
	$(COMPOSE) down

clean: ## derruba tudo e apaga volumes, imagens locais e o binário do cliente
	$(COMPOSE) down -v --rmi local --remove-orphans
	rm -rf bin

logs: ## acompanha os logs (SERVICE=media-relay para um serviço só)
	$(COMPOSE) logs -f $(SERVICE)

ps: ## estado dos contêineres
	$(COMPOSE) ps

_need_host:
	@test -n "$(RELAY_HOST)" || { echo "defina RELAY_HOST=<IP desta máquina na rede>"; exit 1; }

# --- chamadas (cliente de voz no terminal; precisa do sox) ---

call: _need_me ## cria uma chamada: ME=ana [INVITE=bob,carl] [MAX=5]
	exec $(CLIENT) $(CLIENT_FLAGS) -create $(if $(MAX),-max $(MAX)) $(if $(INVITE),-invite $(INVITE))

join: _need_me ## entra numa chamada: ME=bob CALL=<callId>
	@test -n "$(CALL)" || { echo "informe CALL=<callId>"; exit 1; }
	exec $(CLIENT) $(CLIENT_FLAGS) -join $(CALL)

listen: _need_me ## espera um convite e entra: ME=carl
	exec $(CLIENT) $(CLIENT_FLAGS) -listen

mute: _need_me ## liga/desliga o microfone de quem está na chamada: ME=ana
	@pkill -USR1 -f -- "-user $(ME)( |$$)" && echo "mute alternado para $(ME)" || echo "$(ME) não está em chamada nesta máquina"

deafen: _need_me ## liga/desliga o áudio recebido (e o microfone): ME=ana
	@pkill -USR2 -f -- "-user $(ME)( |$$)" && echo "deafen alternado para $(ME)" || echo "$(ME) não está em chamada nesta máquina"

invite: _need_me ## convida alguém para a chamada: ME=ana CALL=<callId> TO=bob
	@curl -s -XPOST $(SERVER)/calls/$(CALL)/invite -d '{"userId":"$(ME)","inviteeId":"$(TO)"}'; echo

leave: _need_me ## sai da chamada: ME=ana CALL=<callId>
	@curl -s -XPOST $(SERVER)/calls/$(CALL)/leave -d '{"userId":"$(ME)"}'; echo

status: ## estado de uma chamada: CALL=<callId>
	@curl -s $(SERVER)/calls/$(CALL); echo

_need_me:
	@test -n "$(ME)" || { echo "informe ME=<seu usuário>"; exit 1; }

# --- testes ---

test: ## testes unitários Go (os 3 módulos)
	cd services/call-signaling && go test ./...
	cd services/media-relay && go test ./...
	cd tools/voice-client && go test ./...

test_case%: ## cenário de falha da especificação: make test_case1 ... make test_case8
	@test -x scripts/demo/f$*.sh || { echo "cenários: test_case1 a test_case8"; exit 1; }
	@scripts/demo/f$*.sh
