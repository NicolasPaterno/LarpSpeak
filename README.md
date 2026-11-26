# Larpspeak

Sistema distribuído de chamadas de voz em grupo (no estilo TeamSpeak/Discord) — trabalho final de
Sistemas Distribuídos, FURB. Três microsserviços (Go + C#) comunicando-se via SNS/SQS, com DynamoDB,
S3 e Secrets Manager emulados localmente pelo [MiniStack](https://github.com/ministackorg/ministack),
e um cliente web para conversar pelo navegador.

| Serviço | Linguagem | Papel |
|---|---|---|
| call-signaling | Go | Contas e login (JWT), API das chamadas, saga de criação, vagas, notificações (SSE) |
| media-relay | Go | Encaminha o áudio: UDP (cliente de terminal) e WebRTC (navegador); réplicas com eleição de líder e failover |
| audit-vault | C# | Auditoria das chamadas e criptografia dos áudios no S3 |
| web | TypeScript (cliente) | Interface no navegador servida por nginx, que também repassa `/api` ao call-signaling; não conta como microsserviço |

## Requisitos

| Ferramenta | Para quê | Obrigatória? |
|---|---|---|
| Docker com Compose v2 | sobe todos os serviços e o MiniStack | sim |
| `git`, `make`, `curl`, `jq`, `perl` | comandos `make` e cenários de teste | sim |
| Go 1.27 | `make test` e os cenários do audit-vault (`make test_audit`, que compilam `tools/webrtc-probe`) | só para esses testes |
| `sox` | microfone e alto-falante do cliente de voz de terminal | só para o cliente de terminal |
| `mkcert` | HTTPS para o microfone do navegador em **outras** máquinas | só para chamadas entre máquinas |

Node e o SDK do .NET não são necessários: o cliente web e o audit-vault são compilados dentro do Docker. O
navegador (Chrome, Edge ou Firefox) já traz o WebRTC e o Opus; não há biblioteca de áudio para instalar
para as chamadas pelo navegador.

## Instalação passo a passo

### macOS

1. Instale o [Docker Desktop](https://www.docker.com/products/docker-desktop/) e abra-o uma vez.
2. Instale o [Homebrew](https://brew.sh) e as ferramentas (`make`, `curl` e `perl` já vêm no macOS):
   ```sh
   brew install git jq go sox mkcert
   ```
3. Clone e entre no repositório:
   ```sh
   git clone https://github.com/NicolasPaterno/LarpSpeak.git
   cd LarpSpeak
   ```
4. Compile e suba (o `make start` descobre o IP do Wi-Fi sozinho):
   ```sh
   make build
   make start
   ```
5. Abra http://localhost:8080.

### Windows (10 ou 11)

Os comandos `make` e os scripts são Bash; no Windows eles rodam no WSL 2 (Ubuntu).

1. No PowerShell como administrador, instale o WSL com Ubuntu e reinicie o computador:
   ```powershell
   wsl --install -d Ubuntu
   ```
2. Instale o [Docker Desktop](https://www.docker.com/products/docker-desktop/). Em *Settings > General*,
   marque *Use the WSL 2 based engine*; em *Settings > Resources > WSL integration*, ative o **Ubuntu**.
3. Abra o terminal **Ubuntu** e instale as ferramentas:
   ```sh
   sudo apt update
   sudo apt install -y git make curl jq perl sox libsox-fmt-pulse mkcert libnss3-tools
   ```
4. Instale o Go 1.27 (o pacote do `apt` é antigo):
   ```sh
   curl -LO https://go.dev/dl/go1.27.1.linux-amd64.tar.gz
   sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.27.1.linux-amd64.tar.gz
   echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc && source ~/.bashrc
   go version
   ```
5. Clone **dentro do Ubuntu** (por exemplo em `~`), não em `C:\`: assim os scripts mantêm o fim de linha
   LF e o Docker fica mais rápido.
   ```sh
   cd ~
   git clone https://github.com/NicolasPaterno/LarpSpeak.git
   cd LarpSpeak
   ```
6. Descubra o IP do Windows na rede: no PowerShell, `ipconfig`, linha *Endereço IPv4* do Wi-Fi ou da
   Ethernet (ex.: `192.168.0.10`). No WSL, o IP detectado automaticamente é o da máquina virtual, que
   outras máquinas não alcançam; por isso informe o IP:
   ```sh
   make build
   make start RELAY_HOST=192.168.0.10     # só nesta máquina: RELAY_HOST=127.0.0.1
   ```
7. Abra http://localhost:8080 no navegador do Windows.
8. Para outras máquinas entrarem, libere no Firewall do Windows (PowerShell como administrador):
   ```powershell
   New-NetFirewallRule -DisplayName larpspeak-tcp -Direction Inbound -Protocol TCP -LocalPort 8080,8081,8443 -Action Allow
   New-NetFirewallRule -DisplayName larpspeak-udp -Direction Inbound -Protocol UDP -LocalPort 40000,40001,50000,50001 -Action Allow
   ```

O cliente de voz de terminal usa o `sox` dentro do WSL; o áudio passa pelo WSLg (Windows 11). No Windows 10
use o navegador, que não depende do `sox`.

### Conferir se está tudo no ar

```sh
make ps                                   # todos os contêineres "running" / "healthy"
curl -s localhost:8081/health             # call-signaling
curl -s localhost:8083/health             # audit-vault
make test_case3                           # um cenário rápido de ponta a ponta
```

### Recriar o ambiente do zero

O MiniStack guarda tabelas, filas, segredos e gravações em memória. Para começar do zero:

```sh
make stop && make start                   # Windows: make start RELAY_HOST=<IP>
```

`make clean && make build && make start` também apaga as imagens e recompila tudo. As tabelas, filas,
tópicos, bucket e segredos são criados pelo `infra/init/init.sh` a cada subida, nunca à mão.

Sem `make`, `docker compose up --build` sobe o sistema inteiro (cenários F1–F8 e API), mas sem as portas UDP de
voz; as chamadas com áudio precisam do `make start`.

## Como executar

```sh
make build     # imagens dos serviços + cliente de voz (bin/voice-client)
make start     # sobe tudo; a voz fica acessível no IP desta máquina na rede
make help      # lista todos os comandos
```

O `make start` detecta o IP da máquina na rede (`RELAY_HOST`). Para usar outro endereço:
`make start RELAY_HOST=192.168.0.10`. Outras configurações (latência e taxa de falha do relay, tamanho
máximo da chamada, réplicas) ficam no `.env` — copie de `.env.example`.

| Comando | O que faz |
|---|---|
| `make stop` | derruba os serviços (o estado do MiniStack fica em memória e zera) |
| `make clean` | derruba tudo e apaga volumes, imagens e o binário do cliente |
| `make logs [SERVICE=media-relay]` | acompanha os logs |
| `make ps` | estado dos contêineres |
| `make test` | testes unitários Go |

| Endpoint | URL |
|---|---|
| cliente web | http://localhost:8080 (HTTPS em `https://<RELAY_HOST>:8443` depois de `make certs`) |
| call-signaling | http://localhost:8081/health |
| audit-vault | http://localhost:8083/health |
| relays de voz, cliente de terminal (UDP) | `<RELAY_HOST>:40000` e `<RELAY_HOST>:40001` |
| relays de voz, navegador (WebRTC/UDP) | `<RELAY_HOST>:50000` e `<RELAY_HOST>:50001` |

## Contas e login

Toda chamada exige login. A conta é criada na tela de cadastro do cliente web ou, nos comandos `make` e
no cliente de terminal, automaticamente no primeiro uso com a senha de `LARPSPEAK_PASSWORD` (padrão
`larpspeak-demo`). Só o call-signaling cria tokens: a chave (Ed25519) fica no Secrets Manager
(`larpspeak/jwt-key`). O token de acesso dura 5 minutos e é renovado sozinho por um cookie httpOnly.

## Chamadas pelo navegador

1. `make start` e abra http://localhost:8080.
2. Crie uma conta, clique em **Abrir canal** e permita o microfone.
3. Envie o link (**Copiar link**) ou convide pelo indicativo. Para testar sozinho com dois usuários na
   mesma máquina, abra o segundo em http://127.0.0.1:8080 (outra origem, outro login) ou numa janela anônima.

Na chamada: `M` liga/desliga o microfone, `D` ensurdece, **Apertar p/ falar** usa a tecla Espaço,
`I` convida, `O` abre o painel de operação (status, relay, RTT, jitter, perda e eventos ao vivo).

Navegadores só liberam o microfone em `https` ou `localhost`. Para pessoas em outras máquinas:

```sh
make certs                                   # certificado local (mkcert) para localhost e RELAY_HOST
docker compose up -d --force-recreate web    # nginx passa a servir HTTPS na porta 8443
```

Nas outras máquinas, abra `https://<RELAY_HOST>:8443` (instale a CA do mkcert ou aceite o aviso do
navegador). O áudio do navegador é sempre criptografado (DTLS-SRTP). O cliente de terminal (PCM) e o
navegador (Opus) não se ouvem na mesma chamada.

## Chamadas de voz

Use fones de ouvido (não há cancelamento de eco). Cada pessoa roda o cliente no próprio terminal:

```sh
make call   ME=ana INVITE=bob,carl MAX=5   # cria a chamada e convida
make listen ME=bob                          # espera o convite e entra
make join   ME=carl CALL=<callId>           # entra direto numa chamada
```

Dentro da chamada, digite no terminal e pressione Enter:

| Tecla | Ação |
|---|---|
| `m` | liga/desliga o microfone |
| `d` | deafen: não ouve ninguém e também silencia o microfone |
| `i <usuário>` | convida alguém para a chamada |
| `q` | sai da chamada |

De outro terminal da mesma máquina:

```sh
make mute   ME=ana                          # alterna o mute da ana
make deafen ME=ana                          # alterna o deafen da ana
make invite ME=ana CALL=<callId> TO=dave    # convida
make leave  ME=ana CALL=<callId>            # sai
make status ME=ana CALL=<callId>            # estado da chamada
```

Opções do cliente: `MUTE=1` e `DEAFEN=1` (já entra assim), `NOMIC=1` (só ouve; para testar sozinho
num computador, rode um segundo cliente com `NOMIC=1`), `LOSS=0.1` (descarta 10% dos pacotes para
ouvir a recuperação de perda), `SERVER=http://<ip>:8081` (cliente em outra máquina).

Para pessoas em outras máquinas da rede, libere no firewall as portas TCP 8080/8443 (cliente web) e 8081
(cliente de terminal) e as portas UDP 40000, 40001, 50000 e 50001. Não exponha a porta 4566
(MiniStack): ela dá acesso total aos dados sem senha. A porta 8081 usa HTTP sem criptografia: senha e
token do cliente de terminal trafegam abertos na rede.

## Cenários de falha (F1–F8)

Cada cenário ajusta a configuração necessária, provoca a falha, mostra logs e estado dos bancos, e
restaura a configuração padrão ao terminar. Rode com o ambiente no ar (`make start`). Os scripts fazem
login como usuários de teste. No macOS, evite que o computador durma durante os cenários longos
(`caffeinate -i make test_case8`): a suspensão congela o Docker e faz a saga expirar.

| Comando | Cenário | O que demonstra |
|---|---|---|
| `make test_case1` | Compensação | relay falhando 100%: chamada FAILED, vaga liberada, cliente notificado por SSE |
| `make test_case2` | Serviço morre no meio | `docker kill` na réplica que processa: mensagem reentregue, uma única alocação |
| `make test_case3` | Mensagem duplicada | mesmo `messageId` 5×: 1 efeito, 4 duplicatas ignoradas |
| `make test_case4` | Mensagem venenosa | payload inválido: 5 tentativas e DLQ (~2,5 min), demais mensagens seguem |
| `make test_case5` | Corrida pelo recurso | 100 joins simultâneos em 10 vagas: exatamente 10 aceitos |
| `make test_case6` | Broker fora do ar | MiniStack pausado 30s: reconexão sozinha, outbox drena, nada perdido |
| `make test_case7` | Resposta tardia | relay mais lento que o timeout: saga compensa e o relay atrasado é liberado |
| `make test_case8` | Escala horizontal | 3 réplicas consumindo a mesma fila: 30 chamadas divididas entre elas, sem duplicidade |

Falha extra para demonstrar o failover dos relays: durante uma chamada, `docker kill` no pod do
relay. O líder detecta em ~3s e move a chamada para o pod menos carregado; o cliente de terminal e o
navegador trocam de relay sozinhos (o painel de operação mostra `relay_moved` e a reconexão).

Para inspecionar a AWS emulada: `scripts/aws.sh sqs list-queues`, `scripts/aws.sh dynamodb list-tables`.

## Documentação

- [ARCHITECTURE.md](ARCHITECTURE.md) — arquitetura completa: serviços, mensagens, decisões (D1–D26), R1–R6 e F1–F8
- [presentation/COMO-FUNCIONA.md](presentation/COMO-FUNCIONA.md) — explicação didática do projeto inteiro
- [presentation/DEMO-LAN.md](presentation/DEMO-LAN.md) — demo com chamada real entre dois computadores
- [docs/architecture.md](docs/architecture.md) — resumo da arquitetura
- [contracts/](contracts/) — esquemas das mensagens
