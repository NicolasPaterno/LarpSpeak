# Roteiro falado — Larpspeak

Roteiro para **um apresentador**, na ordem e com os tempos de `presentation/PLAN.md` (23 slides + apêndices A1–A8).

## Como usar

- **Duração-alvo**: o teto de 20 min vale só para a apresentação do projeto (slides 1–23, 5b e 5c):
  - Fala dos slides: ~955 s (~16 min).
  - Perguntas, demo ao vivo do cenário escolhido (slide 19 + apêndice AN) e falha surpresa **não** contam no tempo.
- **Ritmo**: ~130 palavras por minuto. Cada slide informa o alvo em segundos, o número de palavras da fala e o tempo
  estimado.
- **Convenções**:
  - Texto normal = **fala**, para dizer em voz alta.
  - *[Itálico entre colchetes]* = **ação**: gesto, ponteiro no diagrama, tecla, comando. Não se lê.
  - Rótulos em negrito (**Transição.**, **Previsão.**) só organizam a fala; não se leem.
  - Linhas em citação (`>`), inclusive **Fonte**, e blocos de código são para o apresentador. Não se leem.
  - `[PREENCHER: ...]` = informação que ainda falta (ver `presentation/QUESTIONS.md`).
- Identificadores, tipos de mensagem, comandos, variáveis de ambiente e linhas de log ficam em inglês, como no código.
  Ler os nomes como estão escritos (ex.: "relay-requests", "RELAY_FAILURE_RATE").
- Antes da apresentação: `make build` e `RELAY_HOST=127.0.0.1 make start` (ambiente no ar); terminal aberto na raiz do
  repositório; deck aberto no slide 1.

---

## Slide 1 — Capa (alvo 15 s · 33 palavras · ~15 s)

Boa tarde, professor; boa tarde, pessoal. Somos Nícolas Paterno, William Barboza, Gustavo Amaro e Gustavo Luchini. Vamos apresentar o
Larpspeak, um backend distribuído de chamadas de voz em grupo: o nosso trabalho final de Sistemas Distribuídos.

> Fonte: `txt:2-6`; `ARCHITECTURE.md` §1; `README.md` (primeira linha).

## Slide 2 — Tema e fluxo principal (alvo 45 s · 97 palavras · ~45 s)

O tema registrado é streaming e orquestração de voz em tempo real: chamadas de voz em grupo, no estilo TeamSpeak ou
Discord. A regra de negócio é simples; o foco é o comportamento quando algo falha.

*[aponte para o passo 1 da linha do tempo]* O fluxo principal é criar, entrar e falar. O POST /calls responde na
hora 202, com o callId e o status PENDING. *[passo 2]* O media-relay aloca o relay, responde
RelayAllocated, e a chamada vira ACTIVE; o cliente recebe call_active por SSE. *[passo 3]* Depois, o cliente entra com
join, envia a oferta WebRTC, e o áudio vai direto ao media-relay.

**Transição.** Nesse fluxo estão os três elementos exigidos.

> Fonte: `ARCHITECTURE.md` §1, §8.1, §8.3; `CS/internal/adapter/httpapi/handlers.go:46-82`.

## Slide 3 — Os três elementos exigidos (alvo 45 s · 94 palavras · ~43 s)

*[coluna 1]* Primeiro, a transação distribuída com compensação: a criação da chamada atravessa o call-signaling e o
media-relay. Se o relay responde RelayFailed, ou demora mais que o timeout, a chamada vira FAILED, as vagas e a
reserva do usuário são liberadas, e o cliente recebe call_failed.

*[coluna 2]* Segundo, a dependência instável: a própria alocação do relay, com latência e taxa de falha configuráveis
por RELAY_LATENCY_MS e RELAY_FAILURE_RATE.

*[coluna 3]* Terceiro, o evento com vários interessados: os fatos da chamada, como CallStarted ou CallEnded, são
publicados uma vez e consumidos pelo media-relay e pelo audit-vault.

**Transição.** Agora, o diagrama completo.

> Fonte: `ARCHITECTURE.md` §1.2; `CS/internal/app/saga.go:159-171`; `MR/internal/app/relay.go:176-219`.

## Slide 4 — Diagrama da arquitetura (alvo 55 s · 119 palavras · ~55 s)

*[aponte para os três serviços]* São três microsserviços coordenados por eventos: call-signaling com uma réplica,
media-relay com duas e audit-vault com duas. *[aponte para os cilindros]* Cada um tem as próprias tabelas DynamoDB, e ninguém acessa as do
vizinho.

*[aponte para o centro]* No meio, a mensageria. O call-signaling pede o relay pela fila relay-requests; o media-relay
responde por relay-results. Os fatos da chamada saem no tópico SNS call-events, que alimenta uma fila de cada
consumidor. E os trechos de gravação vão pela fila audio-chunks ao audit-vault.

*[canto inferior direito]* Na nuvem auxiliar: S3 para as gravações cifradas e Secrets Manager para as chaves.

*[aponte para as duas setas HTTP entre serviços]* Entre serviços há só duas chamadas HTTP: o repasse da oferta WebRTC e
a busca da chave pública. O web é só um proxy nginx. *[moldura]* Toda a AWS roda no MiniStack.

> Fonte: `ARCHITECTURE.md` §1.1, §1.3, D11; `docker-compose.yml:75, 99`; `infra/init/init.sh`.

## Slide 5 — Serviços e linguagens (alvo 20 s · 44 palavras · ~20 s)

São três microsserviços em duas linguagens. *[cartões Go]* Os dois serviços de tempo real, sinalização e mídia, são em
Go 1.27, organizados em portas e adaptadores. *[cartão C#]* A auditoria é em C#, com .NET 10. *[faixa contracts/]* Não
há código compartilhado: o único ponto comum é a pasta contracts.

> Fonte: `txt:12`; `ARCHITECTURE.md` §1, §9.1, §12.1, D16; `CS/main.go`, `MR/main.go`, `AV/Program.cs`.
> Por que Go e por que C#, se perguntado: Go fica no caminho de tempo real (goroutines baratas, uma por laço de consumo,
> outbox, batimento e encaminhamento de áudio; `select` não bloqueante no gravador); C# fica fora dele, na auditoria
> (AES-GCM e AWS SDK maduros, `BackgroundService`). `COMO-FUNCIONA.md` §3.1–3.3.
> Se perguntarem "o enunciado pede três linguagens": a introdução (`txt:12`) diz "no mínimo duas linguagens" e as
> Regras (`txt:64`) dizem três. Seguimos a introdução. Responder isso com calma, sem levar ao slide.

## Slide 5b — Áudio: WebRTC, SFU e Opus (alvo 40 s · 88 palavras · ~40 s)

*[aponte para o navegador]* O áudio do navegador usa WebRTC, a única forma de um navegador mandar áudio por UDP. Ele já
traz criptografia obrigatória, DTLS-SRTP, e o codec Opus. *[aponte para o SFU]* O media-relay é um SFU: recebe o Opus de
cada pessoa e só copia os pacotes para as outras, sem decodificar. Por isso o servidor pode gravar e trocar de pod no
failover. *[selo Opus]* O Opus usa quadros de 20 milissegundos e FEC, que reconstrói um pacote perdido. Ele não
retransmite e não cancela eco: isso é do navegador.

> Fonte: `COMO-FUNCIONA.md` §6.2, §7.2–7.4; `MR/internal/adapter/webrtc/sfu.go:1-4, 90-97`.
> Mesh descartado: sem gravação no servidor e com NAT entre pares. MCU descartado: decodifica, mixa e recodifica (CPU,
> latência, perda de qualidade). DTX desligado de propósito: a presença depende de pacotes chegando (D13).

## Slide 5c — Do navegador ao servidor: oferta WebRTC e porta UDP (alvo 55 s · 122 palavras · ~56 s)

*[passo 1]* O navegador abre o microfone e cria a conexão WebRTC. Ele manda a oferta, um texto SDP, ao call-signaling
por HTTP. *[passo 2]* O call-signaling confere a chamada e repassa a oferta ao pod dono, com um token de um minuto. O
pod responde com um único endereço: o IP da máquina, na porta UDP 50000. *[passo 3]* A partir daí, o áudio vai direto
ao pod, sem passar pelo nginx: teste ICE, troca de chaves DTLS, e um pacote Opus cifrado a cada 20 milissegundos.
*[moldura Docker]* Por que publicar a porta? O pod roda dentro do Docker, e o navegador não alcança a rede interna.
O make start publica uma porta UDP por pod, 50000 e 50001, e o RELAY_HOST diz qual IP anunciar.

> Fonte: `COMO-FUNCIONA.md` §7.5; `web/src/voice.ts:96-123`; `CS/internal/app/saga.go:320-332`;
> `CS/internal/adapter/relayhttp/client.go:21-43`; `MR/internal/adapter/httpapi/health.go:26-57`;
> `MR/internal/adapter/webrtc/sfu.go:102-107` (`SetICEUDPMux`, `SetNAT1To1IPs`); `compose.voice.yml`.
> Se perguntarem:
> - Por que uma porta só por pod: todos os navegadores do pod dividem um socket UDP (`SetICEUDPMux`). Uma porta por
>   pod é publicável; uma por participante não.
> - Por que dois serviços no modo voz (`media-relay` e `media-relay-2`): o Docker não publica a mesma porta para duas
>   réplicas. Cada pod a mais precisa de um serviço com porta própria (`media-relay-3` em 40002/50002, e assim por
>   diante). No `docker compose up` puro não há porta UDP publicada: a API e os cenários F1–F8 funcionam, o áudio do
>   navegador não.
> - Sem STUN/TURN e sem trickle: o servidor tem endereço conhecido; só há candidato `host`.
> - Limitação: só rede local ou IP público direto. Rede que bloqueia UDP precisaria de TURN.
> - Autoescala: é bônus do enunciado (`txt:99`), não obrigatória, e não foi implementada.

## Slide 6 — Serviços de nuvem e justificativas (alvo 75 s · 161 palavras · ~74 s)

Usamos quatro categorias de nuvem, todas emuladas pelo MiniStack.

*[linha Mensageria]* Mensageria: SNS e SQS. O SNS distribui um fato para uma fila por consumidor; o SQS dá consumidores
concorrentes, reentrega e DLQ nativa. RabbitMQ ou Kafka em contêiner seriam mais um componente para operar, sem ganho
neste volume.

*[linha Banco]* Banco: DynamoDB. A escrita condicional conta vagas sem lock, e a TransactWriteItems grava efeito,
marcador e outbox de forma atômica. Um relacional exigiria lock próprio e um servidor.

*[linha Objetos]* Objetos: S3, porque áudio é binário e grande, e um item do DynamoDB tem limite de 400 KB. O
media-relay grava trechos de cinco segundos e manda pela fila audio-chunks; o audit-vault cifra cada trecho com
AES-256-GCM e guarda no S3. O callId, o usuário e o início do trecho entram como dado autenticado: um trecho trocado de
pasta não decifra.

*[linha Segredos]* Segredos: o Secrets Manager guarda a chave do JWT e a chave AES do áudio. Em variável de ambiente,
vazariam no docker inspect.

> Fonte: `txt:28, 30`; `ARCHITECTURE.md` §4; `infra/init/init.sh:43-63`; `COMO-FUNCIONA.md` §8.1, §8.4.
> Prova ao vivo, se pedirem: `make recording` (cabeçalho `LSA1`, 401 sem token, 500 `recording failed authentication`
> com trecho trocado de pasta, um `.ogg` por participante). Ver `presentation/DEMO-LAN.md` §4.

## Slide 7 — Broker: SNS + SQS e fan-out (alvo 40 s · 85 palavras · ~39 s)

Separamos dois tipos de mensagem. *[aponte para o hexágono call-events]* Fatos, como CallStarted ou CallEnded, vão ao
tópico call-events, que entrega em uma fila por consumidor. Só o call-signaling publica no tópico. *[aponte para as duas filas]* Se o audit-vault cair, as
mensagens esperam na fila dele, e o media-relay nem percebe. Adicionar um consumidor não muda quem publica.

*[aponte para o par atenuado]* Comandos e respostas têm um único executor, então andam em filas ponto a ponto:
relay-requests e relay-results.

*[aponte para as caixas -dlq]* Toda fila tem DLQ, maxReceiveCount cinco e visibility timeout de trinta segundos: uma
mensagem ruim não trava a fila.

> Fonte: `txt:22, 66`; `ARCHITECTURE.md` §2.1, §2.3, D1, D2; `infra/init/init.sh:6-36`;
> `CS/internal/adapter/messaging/bus.go:140-151`.

## Slide 8 — Banco por serviço (alvo 30 s · 67 palavras · ~31 s)

Cada serviço lê e escreve só as próprias tabelas. *[aponte para as três colunas]* O call-signaling usa as tabelas
signaling, o media-relay as tabelas relay, e o audit-vault as tabelas audit e o bucket de áudio. Os nomes vêm de
variáveis de ambiente, como CALLS_TABLE. *[aponte para a seta com X]* Nenhum serviço consulta o banco do vizinho:
informação entre serviços passa só por mensagem. Por exemplo, o audit-vault monta a trilha só com os eventos que
recebe.

> Fonte: `txt:65`; `ARCHITECTURE.md` §3; `docker-compose.yml:46-51, 88-91, 105-106`.

## Slide 9 — Contratos e envelope (alvo 30 s · 66 palavras · ~30 s)

Os contratos estão na pasta contracts, em JSON Schema: o envelope e doze schemas, um por tipo de mensagem.
*[aponte para o bloco JSON]* Toda mensagem usa o mesmo envelope: messageId, correlationId, type, version, occurredAt e
payload. O messageId é a chave de idempotência; o correlationId é o callId, para rastrear a chamada em todos os serviços. Um envelope
inválido é mensagem venenosa: não é apagado e vai para a DLQ.

> Fonte: `txt:67`; `ARCHITECTURE.md` §2.2, §2.3, D3; `contracts/`; `CS/internal/domain/envelope.go:21-52`;
> `AV/Envelope.cs:19-52`.

## Slide 10 — Contêineres, infraestrutura por script, MiniStack (alvo 45 s · 97 palavras · ~45 s)

Cada serviço tem Dockerfile e um GET /health, que o Docker consulta no healthcheck. *[aponte para a ordem de partida]* Tudo sobe com docker compose up
--build. Primeiro, o MiniStack; quando ele fica saudável, o contêiner infra-init roda o script init.sh, que cria
filas, DLQs, tópico, tabelas, bucket e segredos. O script é versionado e idempotente; nada é criado à mão. Só quando
ele termina os serviços partem.

*[aponte para o trecho de env]* Endpoint, credenciais, filas e tabelas vêm de variáveis de ambiente, nunca do código. AWS_ENDPOINT_URL
aponta para o MiniStack; na AWS real, muda só essa variável.

**Transição.** Com a base montada, vamos às garantias, começando pela saga.

> Fonte: `txt:68, 105, 112-116`; `ARCHITECTURE.md` §1.3, D26; `docker-compose.yml:1-13`; `infra/init/init.sh`.
> Se perguntarem como recriar do zero: `make stop && make start` (README, "Recriar o ambiente do zero"); sem `make`,
> `docker compose down -v && docker compose up --build`.

## Slide 11 — Saga com compensação (alvo 50 s · 109 palavras · ~50 s)

O call-signaling orquestra a saga: ele é o único dono do estado da chamada, o que simplifica a
compensação. *[máquina de estados]* A chamada nasce
PENDING. Com RelayAllocated, vira ACTIVE. Com RelayFailed, ou quando o varredor SweepTimeouts acha uma chamada PENDING
mais velha que RELAY_TIMEOUT_MS, três segundos, vira FAILED.

*[sequência à direita]* A compensação zera as vagas, libera a reserva do usuário, avisa o criador com call_failed e
publica CallFailed. Como Fail só vale com status PENDING, ela roda no máximo uma vez.

*[ramo pontilhado]* E se o RelayAllocated chega depois do timeout? A chamada já está FAILED; o call-signaling manda
ReleaseRelayRequested, e o media-relay libera a sessão com motivo late allocation. Esse é o cenário F7.

> Fonte: `txt:20`; `ARCHITECTURE.md` §6.4, §8.2, D8; `CS/internal/app/saga.go` (`compensate` 159-171, resposta
> atrasada 140-150, `SweepTimeouts` 376-397).

## Slide 12 — R1: pelo menos uma vez + idempotência (alvo 50 s · 107 palavras · ~49 s)

R1. O SQS entrega pelo menos uma vez; quem precisa ser idempotente é o efeito. *[caixa TransactWriteItems]* Cada
consumidor grava numa única TransactWriteItems: no item zero, o marcador com o messageId, com attribute_not_exists;
depois, o efeito; e, quando há, a próxima mensagem no outbox.

*[duas setas com o mesmo messageId]* Se a mensagem chega de novo, a transação falha no item zero; o consumidor registra
duplicate ignored e apaga a mensagem.

*[RelayOutbox]* O outbox resolve o outro lado: estado e mensagem saem na mesma transação, e uma goroutine varre a
tabela a cada 200 milissegundos, publica e apaga o item. O código está em store.go, sessions.go e DynamoAuditStore. O áudio é a exceção:
chave S3 determinística.

> Fonte: `txt:77-79`; `ARCHITECTURE.md` §5.1, §5.2, §10, D4, D5, D19; `CS/internal/adapter/dynamo/store.go:393`
> (`commit`); `MR/internal/adapter/dynamo/sessions.go:163-178`; `AV/DynamoAuditStore.cs` (`RecordAsync`);
> `CS/internal/adapter/messaging/bus.go:114-138`. D19 (áudio): PUT com chave determinística, depois o marcador; quem perde a corrida do marcador loga `duplicate ignored`
> (`AV/AudioProcessor.cs:41-44`).

## Slide 13 — R2: controle de concorrência (alvo 35 s · 79 palavras · ~36 s)

R2. O recurso escasso são as vagas da chamada. *[aponte para o cilindro signaling-calls]* O join é um Update
condicional: status ACTIVE, taken menor que capacity, e o usuário ainda fora de participants. Na mesma transação,
reservamos o usuário em signaling-users: uma chamada por usuário. *[setas recusadas]* Se várias requisições disputam a
última vaga, o DynamoDB aceita uma; as outras falham na condição e viram 409, call is full. Sem lock, sem coordenador. A saída usa a mesma ideia: o taken lido fica fixado na condição.

> Fonte: `txt:80-82`; `ARCHITECTURE.md` §6.5, D9, D10; `CS/internal/adapter/dynamo/store.go:182-205` (`Join` 182,
> `Leave` 194; condição em `join()` 300).

## Slide 14 — R3: API assíncrona (alvo 25 s · 57 palavras · ~26 s)

R3. *[sequência]* O POST /calls responde 202 Accepted na hora, com o callId, o status PENDING e a statusUrl, além do
cabeçalho Location. O cliente consulta depois com GET /calls/{id}, ou recebe call_active ou call_failed por SSE. O cliente não fica bloqueado esperando a alocação do relay. Está
em handlers.go, no handleCreate; o 202 sai na linha 82.

> Fonte: `txt:83-85`; `ARCHITECTURE.md` §6.2, §10; `CS/internal/adapter/httpapi/handlers.go:46-82`.

## Slide 15 — R4: consumidores concorrentes e ordenação (alvo 50 s · 108 palavras · ~50 s)

R4. *[fila e réplicas]* O media-relay roda com duas réplicas em relay-requests, e o audit-vault com duas nas suas
filas. O SQS distribui por pull, sem balanceador; está no deploy.replicas do compose.

*[exemplo fora de ordem]* Ordenação: não há ordem garantida. As filas são SQS padrão, as réplicas processam em paralelo
e o outbox publica sem ordem. Em vez de ordenar, cada efeito confere o estado atual: Activate e Fail só com PENDING;
Release e Member só com a sessão ALLOCATED; a trilha do audit-vault é reordenada por occurredAt na leitura. Um
ParticipantJoined que chega depois do CallEnded encontra RELEASED e é descartado. Caso não coberto: Joined e Left do
mesmo usuário invertidos no media-relay.

> Fonte: `txt:86-88`; `ARCHITECTURE.md` §5.5, §8.5, §10; `docker-compose.yml:75, 99`; `AV/AuditModels.cs`
> (`CallTrail.Build`). Com `make start` (modo voz) o media-relay roda como dois serviços de 1 réplica cada
> (`media-relay`, `media-relay-2`, `compose.voice.yml`); só dizer se perguntado.

## Slide 16 — R5: dependência instável simulada (alvo 20 s · 41 palavras · ~19 s)

R5. A dependência instável é a alocação do relay. *[mostrador]* RELAY_LATENCY_MS define a latência, duzentos
milissegundos no compose; RELAY_FAILURE_RATE define a taxa de falha, de zero a um. As duas vêm de variável de ambiente
e são aplicadas no allocate, em relay.go.

> Fonte: `txt:89-91`; `ARCHITECTURE.md` §7.2, §10; `docker-compose.yml:79-80`; `MR/internal/app/relay.go:176-219`.

## Slide 17 — R6: reconexão (alvo 35 s · 79 palavras · ~36 s)

R6. *[linha do tempo]* As tentativas ficam no nosso laço, não no SDK. O cliente AWS não repete e tem timeout de quinze
segundos, maior que o long poll de dez: a queda aparece no log, em vez de prender a conexão. *[logs]* Nos serviços Go
sai receive failed, retrying e outbox scan failed, retrying; no C#, broker unreachable, retrying. O backoff vai de 250
milissegundos a cinco segundos. Quando o broker volta, o consumo recomeça e o outbox drena, sem reinício manual.

> Fonte: `txt:92-94`; `ARCHITECTURE.md` §5.3, §10, D7; `CS/internal/adapter/messaging/bus.go` (`backoff` 54-61,
> `Consume` 72-110, linhas 79/87/119); `CS/main.go:49-50`; `AV/QueueConsumer.cs:67`.

## Slide 18 — Failover do media-relay (alvo 45 s · 99 palavras · ~46 s)

Esta é a decisão mais distribuída do projeto. Cada pod grava um batimento por segundo em relay-nodes. Um líder, eleito
por lease com um term, é o único que declara um pod morto. *[seta 1]* Morto é sem batimento há mais de três segundos e
sem resposta no /health. *[seta 2]* O líder marca o pod como morto e move a chamada para o pod com menos sessões. As
duas escritas são condicionadas ao term: é o fencing, que impede um líder antigo de agir. *[setas 3 e 4]* RelayMoved
chega ao call-signaling, que avisa o navegador com relay_moved. *[seta 5]* O navegador refaz a oferta no novo pod.

> Fonte: `ARCHITECTURE.md` §7.6, D12; `MR/internal/app/failover.go`; `MR/internal/adapter/dynamo/nodes.go` (`Claim` 56,
> `MarkDead` 94, `Move` 118).
> Prova ao vivo, se pedirem, com uma chamada real no navegador: `make kill_relay` (mata o pod dono da chamada, mostra
> `chamada movida: X -> Y` e os logs `relay pod dead` / `call moved` / `picked up`; o áudio some 3–5 s e volta).
> Ver `presentation/DEMO-LAN.md` §3.
> História real, se couber no Q&A: quem saía e voltava ficava mudo. O slot do SFU reaproveitava o mesmo SSRC com
> sequência e timestamp novos, e o buffer de jitter do Chrome descartava os pacotes. `slot.rewrite` continua a
> numeração (`MR/internal/adapter/webrtc/sfu.go:77`, `sfu_slot_test.go`).

## Slide 19 — F1–F8 e demo ao vivo (alvo 30 s de menu · 62 palavras · ~29 s; demo fora do tempo)

Os oito cenários de falha têm um comando cada. *[percorra a grade]* F1 e F7 mexem na dependência; F2 e F8, nas réplicas
do media-relay; F3 e F4 injetam mensagens em relay-results; F5 disputa as vagas; e F6 pausa o MiniStack. Depois da
apresentação, o senhor escolhe um cenário, e rodamos o script ao vivo, mostrando o resultado nos logs e no estado do
banco.

*[siga para o slide 20; a escolha do cenário vem depois do slide 23]*

> Comando ao vivo: `RELAY_HOST=127.0.0.1 scripts/demo/fN.sh`. Evidência completa, com audit-vault:
> `make test_caseN` (só se o professor pedir; pelo menos dobra o tempo). Fonte: `txt:120`; `ARCHITECTURE.md` §11;
> `Makefile` (`test_case%`); `scripts/demo/lib.sh`, `f1.sh`…`f8.sh`.

## Slide 20 — Falha surpresa: previsão (alvo 40 s · 90 palavras · ~42 s)

O enunciado pede prever a falha surpresa antes de executar. *[tabela, linha call-signaling]* Sem o call-signaling, a
API para e o nginx responde 502, mas o áudio das chamadas ativas continua, porque vai direto ao relay. Na volta, o
outbox publica o que ficou e o varredor compensa as chamadas PENDING. *[linhas seguintes]* Sem um pod do media-relay,
o líder move as chamadas em cerca de três segundos. Sem o audit-vault, os eventos esperam na fila dele. Com o MiniStack
pausado, os laços voltam sozinhos, como no F6. Temos scripts prontos para o call-signaling e para o pod do relay.

*[se o professor pedir a falha surpresa agora, vá à seção "Falha surpresa" abaixo]*

> Fonte: `txt:159`; `ARCHITECTURE.md` §11.1; `scripts/demo/surprise-kill-call-signaling.sh`; `make kill_relay`
> (`scripts/demo/kill-call-relay.sh`, precisa de uma chamada ativa).

## Slide 21 — Decisões de arquitetura (alvo 30 s · 65 palavras · ~30 s)

Recapitulando as decisões, com a alternativa descartada. *[percorra os cartões]* Fan-out com fila por consumidor, em
vez de publicar em cada fila. Marcador e efeito na mesma transação, em vez de SQS FIFO, que deduplica só em cinco
minutos. Outbox, em vez de publicar após o commit. Saga orquestrada, em vez de coreografia. Pod dono por chamada, em
vez de balanceador. Lease com fencing, em vez de Raft.

> Fonte: `ARCHITECTURE.md` §13 (D1, D4, D5, D8, D11, D12).

## Slide 22 — Limitações conhecidas (alvo 30 s · 64 palavras · ~30 s)

Também temos limitações conhecidas. *[marcadores no diagrama]* O call-signaling roda com uma réplica, porque o hub de
SSE fica em memória. O MiniStack guarda tudo em memória: um down apaga o estado. O outbox não tem ordem. Os marcadores de idempotência não têm
TTL. A gravação é melhor esforço e só cobre o áudio do navegador. E qualquer usuário autenticado entra numa chamada
ACTIVE que tenha vaga.

> Fonte: `ARCHITECTURE.md` §14, D15, D20; `COMO-FUNCIONA.md` §16.6. Outras, só se perguntado: a chave AES não tem
> rotação; `ForgetUser` roda fora da transação (o próprio join limpa o registro velho); credenciais `test/test` do
> MiniStack; o audit-vault publica só 2 portas (8083–8084), então roda com no máximo 2 réplicas; num failover, até 5 s
> da gravação se perdem com o pod morto.
> Bônus (`txt:96-101`): nenhum foi implementado; dizer só se perguntado. O `init.sh` é script de AWS CLI, não
> Terraform/CDK; a API de leitura é separada, mas não é CQRS; o rastreio é pelo `correlationId` nos logs, sem OTel; os
> eventos só têm a v1.

## Slide 23 — Encerramento (alvo 15 s · 37 palavras · ~17 s)

Resumindo: três serviços, duas linguagens, quatro categorias de nuvem, R1 a R6 apontados no código e F1 a F8 com um
comando cada. O código está em github.com/NicolasPaterno/LarpSpeak. Obrigado; ficamos abertos a perguntas.

*[pergunte qual cenário F1–F8 o professor escolhe; clique no cartão FN do slide 19 para abrir o apêndice AN e siga
a seção "Demo ao vivo" abaixo]*

> Fonte: `ARCHITECTURE.md` §1, §10, §11; `txt:163`.

---

## Demo ao vivo (slides A1–A8)

Roteiro comum a todos os cenários:

1. *[abra o slide AN]* Dizer o que vai ser provocado.
2. Dizer a **previsão antes** de apertar Enter.
3. *[rode o comando]* Falar o texto de "Enquanto roda".
4. Mostrar a linha `✔` final, a linha de log e o estado do banco indicados.
5. Dizer a conclusão.

Os scripts restauram a configuração padrão ao terminar (`restore` em `scripts/demo/lib.sh`), menos `f4.sh`, que não
altera configuração. A demo não conta no tempo da apresentação.

Todo script mostra ao vivo linhas com hora (`[HH:MM:SS.mmm]`), as linhas reais de log dos serviços (`│ hora contêiner
mensagem`) e termina num bloco `== Evidência` com `banco:` (estado lido do DynamoDB/SQS), `log:` e a linha `✔`. Cada
`✔` sai de uma checagem: se o resultado esperado não acontece, o script para com `✘`.

### A1 — F1 Compensação (fala 109 palavras · ~50 s; script ~30 s)

**Provocar.** Vamos colocar a dependência em 100% de falha: dois relays com RELAY_FAILURE_RATE igual a um.

**Previsão.** Antes de rodar: a chamada da ana vai para FAILED em poucos segundos; ela sai de signaling-users; recebe
call_failed por SSE; o relay registra a falha simulada e o call-signaling registra call compensated. No audit-vault, a
trilha fica FAILED, com motivo relay failed e um único evento, CallFailed, verificado.

> ```
> RELAY_HOST=127.0.0.1 scripts/demo/f1.sh
> ```

**Enquanto roda.** O script recria os relays com a variável, cria a chamada e escuta as notificações. A compensação é
a do slide 11: Fail só vale com PENDING, então roda uma única vez.

> Mostrar: os passos da saga ao vivo (`allocating relay`, `relay allocation failed (simulated)`, `call compensated`) e
> a hora em que o SSE `call_failed` chegou; na Evidência, `signaling-calls: ... taken=0 status=FAILED reason=relay
> failed: simulated relay failure`, `signaling-users (ana-…): (nenhum item)`; `✔ F1: compensação executada`.

**Conclusão.** Passos desfeitos, recurso liberado, cliente notificado: é o resultado que o F1 exige.

> Fonte: `scripts/demo/f1.sh`, `lib.sh` (`audit_failed`); `CS/internal/app/saga.go:95, 159-171`;
> `MR/internal/app/relay.go:188-196`; `ARCHITECTURE.md` §11.

### A2 — F2 Serviço morre no meio (fala 119 palavras · ~55 s; script ~60–75 s)

**Provocar.** Vamos matar o consumidor no meio do processamento. O script deixa a alocação lenta, oito segundos, acha
a réplica do media-relay que registrou allocating relay e faz docker kill nela.

**Previsão.** A mensagem não foi apagada, porque só apagamos depois do efeito. Ela volta depois do visibility timeout
de trinta segundos, a outra réplica aloca, e a chamada fica ACTIVE. Esperamos exatamente uma linha relay allocated.

> ```
> RELAY_HOST=127.0.0.1 scripts/demo/f2.sh
> ```

**Enquanto roda.** *[aponte para o nome da réplica morta]* Agora esperamos uns trinta segundos. Se a réplica morta
tivesse chegado a gravar, o marcador em relay-processed faria a segunda réplica descartar a mensagem como duplicata.
Efeito e marcador vão na mesma transação: ou os dois existem, ou nenhum.

> Mostrar, na ordem: a linha `allocating relay ... messageId=<id>` da réplica; `docker kill` com hora e `exit=137`;
> `subindo consumer de novo: docker start ...` até `running healthy`; a fila `relay-requests: visible=0 inflight=1`;
> o mesmo `messageId` em `allocating relay` de novo (reentrega) e `relay allocated` uma vez; na Evidência,
> `relay-sessions (<id>): 1 item(s)` com owner e state ALLOCATED, `'allocating relay' 2x` e `'relay allocated' 1x`;
> `✔ F2: chamada ACTIVE, 1 alocação, réplica morta não deixou efeito duplicado`.
> A reentrega pode cair na própria réplica reiniciada ou na outra; as duas valem pelo enunciado ("reentregue a outra
> réplica ou após o reinício").

**Conclusão.** A mensagem foi reentregue a outra réplica, e o efeito foi aplicado uma única vez.

> Fonte: `scripts/demo/f2.sh`; `ARCHITECTURE.md` §5.1, §11, D6; `MR/internal/app/relay.go:181, 217`.

### A3 — F3 Mensagem duplicada (fala 71 palavras · ~33 s; script ~30 s)

**Provocar.** Com os relays parados, a chamada fica PENDING. Vamos publicar o mesmo RelayAllocated, com o mesmo
messageId, cinco vezes em relay-results.

**Previsão.** Uma única ativação: uma linha call active e quatro duplicate ignored no call-signaling, e um marcador com
esse messageId em signaling-processed.

> ```
> RELAY_HOST=127.0.0.1 scripts/demo/f3.sh
> ```

**Enquanto roda.** A primeira entrega grava o marcador e ativa a chamada na mesma transação. As outras quatro falham no
item zero, o marcador, e são apagadas.

> Mostrar: os 5 envios com o MessageId do SQS de cada um; ao vivo, 1 `call active` e 4 `duplicate ignored`; na
> Evidência, `signaling-calls: status=ACTIVE`, `signaling-processed: pk=<messageId>`, `efeitos ('call active'): 1 |
> 'duplicate ignored' ...: 4`; `✔ F3: 5 entregas, 1 efeito`.

**Conclusão.** Cinco entregas, um efeito.

> Fonte: `scripts/demo/f3.sh`; `CS/internal/adapter/dynamo/store.go:393-411`;
> `CS/internal/adapter/messaging/bus.go:100`; `ARCHITECTURE.md` §5.1.

### A4 — F4 Mensagem venenosa (fala 106 palavras · ~49 s; script ~2,5–3 min)

**Provocar.** Vamos publicar em relay-results um texto que não é envelope.

**Previsão.** O call-signaling não consegue ler o envelope, registra poison message left for DLQ e não apaga a
mensagem. Ela volta a cada trinta segundos; depois de cinco recebimentos, uns dois minutos e meio, vai para a
relay-results-dlq. Enquanto isso, outra chamada chega a ACTIVE normalmente.

> ```
> RELAY_HOST=127.0.0.1 scripts/demo/f4.sh
> ```

**Enquanto roda.** *[mostre `✔ chamada <id> ficou ACTIVE`]* A chamada normal já ficou ACTIVE: a mensagem ruim não
trava a fila. *[mostre cada `tentativa k/5`]* Cada tentativa aparece com a linha real de log do call-signaling, a
cada trinta segundos.

> Mostrar: `maxReceiveCount` e visibility lidos da própria fila; `tentativa 1/5` … `tentativa 5/5`, cada uma com a linha
> real `poison message left for DLQ` (~30 s entre elas); `relay-results-dlq: 1 mensagem(ns)`; na Evidência, o
> MessageId e o body na DLQ (o mesmo que foi publicado); `✔ F4: 5 tentativas, mensagem na DLQ, sistema seguiu
> funcionando`. A chamada de controle pode aparecer ENDED no fim: ela terminou sozinha durante a espera.

**Conclusão.** Cinco tentativas, a mensagem está na DLQ, e o resto do sistema seguiu funcionando.

> Fonte: `scripts/demo/f4.sh`; `CS/internal/adapter/messaging/bus.go:95`; `infra/init/init.sh:6-13`;
> `ARCHITECTURE.md` §2.1, §2.2, D6.

### A5 — F5 Corrida pelo recurso (fala 81 palavras · ~37 s; script ~30 s)

**Provocar.** Vamos criar uma chamada com onze lugares, o criador mais dez vagas, e disparar cem joins simultâneos.

**Previsão.** Exatamente dez respostas 200 e noventa 409. No banco, taken igual a onze, capacity onze, e onze
participantes.

> ```
> RELAY_HOST=127.0.0.1 scripts/demo/f5.sh
> ```

**Enquanto roda.** Os logins são feitos antes, fora da corrida. Os cem joins chegam juntos ao mesmo item; a condição
taken menor que capacity é avaliada pelo DynamoDB a cada escrita, então a décima primeira vaga não existe.

> Mostrar: o placar ao vivo a cada 10 respostas (`100/100 respostas: 200=10 409=90`); na Evidência,
> `signaling-calls depois: taken=11 capacity=11` e os 11 participantes; `✔ F5: 10 joins aceitos (200), 90 recusados
> (409), nenhum lugar a mais`.

**Conclusão.** Exatamente dez sucessos; os outros noventa recusados. Nenhuma venda dupla.

> Fonte: `scripts/demo/f5.sh`; `CS/internal/adapter/dynamo/store.go:300-311` (`join()`); `ARCHITECTURE.md` §6.5.

### A6 — F6 Broker fora do ar (fala 114 palavras · ~53 s; script ~90 s)

**Provocar.** Vamos pausar o MiniStack por trinta segundos, com uma chamada em andamento. Isso derruba SQS, SNS e
DynamoDB de uma vez.

**Previsão.** Durante a queda, um POST /calls falha rápido, com 503. Os serviços registram as novas tentativas. Quando
o MiniStack volta, a chamada criada antes fica ACTIVE, os dois outboxes esvaziam, e uma chamada nova funciona. Ninguém
é reiniciado à mão.

> ```
> RELAY_HOST=127.0.0.1 scripts/demo/f6.sh
> ```

**Enquanto roda.** *[durante a contagem 30 s… 20 s… 10 s]* Agora o broker está fora. O cliente AWS tem timeout de
quinze segundos, então a conexão presa falha e o laço registra o erro. O backoff cresce até cinco segundos. O estado
pendente está guardado no outbox; nada depende de memória.

> Mostrar: `POST /calls durante a queda: HTTP 503`; `broker pausado há Ns | avisos de retry: call-signaling-1=… 
> media-relay-1=…` subindo a cada 5 s; o unpause e `call active` ao vivo; a outbox caindo até 0; na Evidência, o
> primeiro e o último `receive failed, retrying`/`outbox scan failed, retrying` de cada serviço, `signaling-outbox: 0
> itens`, `relay-outbox: 0 itens` e os pods sem reinício (`RestartCount` 0); `✔ F6: serviços reconectaram sozinhos e
> novas chamadas funcionam`.
> Se perguntarem por que `pause` e não `stop`: o MiniStack guarda tudo em memória; um `stop` apagaria filas e tabelas.
> A outbox também está no MiniStack: ela cobre o caso "commit feito, publish falhou".

**Conclusão.** Os serviços reconectaram sozinhos, o outbox drenou e nenhuma mensagem foi perdida.

> Fonte: `scripts/demo/f6.sh`; `ARCHITECTURE.md` §5.3, §11.1 (503 em 3 s), D7; `CS/internal/adapter/messaging/bus.go`.

### A7 — F7 Resposta tardia (fala 99 palavras · ~46 s; script ~40 s)

**Provocar.** Vamos deixar a dependência mais lenta que o timeout: RELAY_LATENCY_MS seis mil, RELAY_TIMEOUT_MS três
mil.

**Previsão.** Em três a quatro segundos, o varredor marca a chamada FAILED, com motivo relay timeout. Aos seis
segundos, o relay termina e manda RelayAllocated. O call-signaling encontra FAILED, registra late relay released e
manda ReleaseRelayRequested. A sessão termina RELEASED, com motivo late allocation, e a trilha fica FAILED, sem
CallStarted.

> ```
> RELAY_HOST=127.0.0.1 scripts/demo/f7.sh
> ```

**Enquanto roda.** A resposta atrasada não reativa a chamada, porque Activate exige PENDING. Ela cai no ramo da
resposta atrasada e vira uma compensação do lado do relay.

> Mostrar: a linha do tempo ao vivo (`call compensated` aos ~3 s, `relay allocated` aos ~6 s, `late relay released`,
> `relay released`); na Evidência, `signaling-calls: status=FAILED reason=relay timeout` e `relay-sessions:
> state=RELEASED reason=late allocation`; `✔ F7: RelayAllocated atrasado gerou ReleaseRelayRequested e o relay foi
> liberado`.

**Conclusão.** O resultado que chegou atrasado foi compensado corretamente.

> Fonte: `scripts/demo/f7.sh`; `CS/internal/app/saga.go:140-150, 376-397`; `MR/internal/app/relay.go:227`;
> `ARCHITECTURE.md` §6.4, §8.2.

### A8 — F8 Escala horizontal (fala 76 palavras · ~35 s; script ~40 s)

**Provocar.** Vamos subir três réplicas do media-relay e criar trinta chamadas.

**Previsão.** As trinta ficam ACTIVE. A carga se divide entre as três réplicas, não necessariamente em partes iguais,
porque o SQS distribui por pull. Nenhuma chamada é alocada duas vezes.

> ```
> RELAY_HOST=127.0.0.1 scripts/demo/f8.sh
> ```

**Enquanto roda.** Não há balanceador: cada réplica puxa mensagens da mesma fila relay-requests. A réplica que recebe o
RelayRequested vira a dona da chamada, e o marcador impede uma segunda alocação.

> Mostrar: o placar ao vivo por réplica até `total 30/30`; na Evidência, `log: larpspeak-media-relay-N: X chamadas` e
> `banco: relay-sessions owner=<hostname> (larpspeak-media-relay-N): X sessões`, contando só as 30 chamadas desta
> rodada; `✔ F8: carga dividida, cada chamada alocada uma única vez` (o script falha se alguma réplica ficar com 0).

**Conclusão.** Carga dividida entre as réplicas, sem efeito duplicado.

> Fonte: `scripts/demo/f8.sh`; `ARCHITECTURE.md` §7.2, §8.5.

### Plano B (se a demo falhar)

**Fala.** O ambiente não respondeu como esperado. Vamos recuperar o ambiente e rodar o cenário de novo; enquanto isso,
explico pelo diagrama onde cada linha vai aparecer.

> Ordem de recuperação (`PLAN.md` §4):
> 1. Script falhou no meio: ele restaura a configuração no `trap`; rodar de novo uma vez.
> 2. Ambiente quebrado: `make stop && RELAY_HOST=127.0.0.1 make start` (demora; o MiniStack zera).
> 3. Sem tempo: explicar pelo diagrama e pelo apêndice AN; não há saída gravada (decisão do responsável).

---

## Falha surpresa

*[se o professor pedir para derrubar um componente: repita o nome, diga a previsão da tabela abaixo, só então execute]*

### Previsões faladas por componente

*[diga só a previsão do componente que o professor escolher: cada uma leva 15–30 s]*

**call-signaling.** É a única réplica. Prevemos: a API inteira para, o nginx responde 502 e as conexões SSE caem. O
contêiner não volta sozinho, porque um docker kill não é reiniciado pelo unless-stopped. O áudio das chamadas ativas
continua, porque vai direto ao relay. Quando ele volta, o outbox publica o que ficou, o varredor compensa as chamadas
PENDING vencidas, e um RelayAllocated atrasado vira ReleaseRelayRequested. O navegador reconecta o SSE sozinho.

**Um pod do media-relay.** Prevemos: para o áudio das chamadas daquele pod. Em cerca de três segundos sem batimento e
sem /health, o líder move as chamadas para o pod com menos sessões, e o cliente recebe relay_moved. A mensagem que o
pod processava volta em trinta segundos para outra réplica, sem efeito duplo. Se ele era o líder, outro pod assume o
lease.

**Todos os media-relay.** Prevemos: novas chamadas ficam PENDING e viram FAILED por relay timeout. As chamadas ACTIVE
continuam ACTIVE no call-signaling, mas sem áudio. Na volta, cada pod recarrega as próprias sessões, e os pedidos que
esperavam na fila são alocados ou liberados.

**audit-vault.** Uma réplica: a outra consome as duas filas, e o que a morta não apagou volta em trinta segundos, sem
duplicata. Todo o audit-vault: trilha e gravações param, mas o resto segue; os eventos se acumulam na fila dele e são
consumidos na volta.

**MiniStack pausado.** Prevemos o F6: tudo que usa AWS para, o POST /calls responde 503, o áudio já estabelecido
continua, e os laços voltam sozinhos.

**MiniStack reiniciado.** É diferente: o estado fica em memória, então tabelas, filas, gravações e chaves se perdem, e o
infra-init não roda de novo. É preciso recriar o ambiente.

**web.** Param a interface e o proxy para o navegador. Continuam o áudio WebRTC já conectado e o voice-client, que fala
direto com o call-signaling. Ao recarregar, o navegador restaura a sessão pelo cookie de refresh.

> Comandos sem script (conferir nomes com `docker compose ps`): `docker kill <contêiner>`; logs com
> `make logs SERVICE=<serviço>`; volta com `docker compose up -d <serviço>`, ou `RELAY_HOST=127.0.0.1 make start`
> (sobe também o `media-relay-2` do modo voz). Pod do relay com chamada real no navegador: `make kill_relay`.
> MiniStack pausado: `docker compose pause ministack` /
> `docker compose unpause ministack`. Evitar reiniciar o MiniStack; se acontecer: `make stop &&
> RELAY_HOST=127.0.0.1 make start` (o `ARCHITECTURE.md` §11.1 cita `docker compose down && docker compose up --build`).
> Fonte: `ARCHITECTURE.md` §11.1, §7.6, D1.

### Demo com script: derrubar o call-signaling

**Previsão (dizer antes de rodar).** Vamos derrubar o call-signaling com uma chamada WebRTC real em andamento, logo
depois de criar outra chamada. Prevemos: o contêiner fica parado e o nginx responde 502. O áudio da chamada em
andamento continua. A chamada criada antes da queda termina por um de dois caminhos, e os dois são corretos. Caminho A:
o varredor vence, a chamada fica FAILED por relay timeout, e o RelayAllocated atrasado é liberado. Caminho B: o relay
responde antes da primeira varredura, e a chamada é ativada uma única vez. Nos dois, a sessão do relay termina
liberada, e nada é aplicado duas vezes.

> ```
> RELAY_HOST=127.0.0.1 scripts/demo/surprise-kill-call-signaling.sh
> ```
> Roda na máquina de desenvolvimento, que já tem Go e `bin/webrtc-probe`. Duração ~1–1,5 min.

**Enquanto roda.** *[passo 3 do script]* O contêiner está exited, e a API pelo nginx dá 502. *[passo 4]* O script sobe o
serviço de novo. Agora o outbox publica o RelayRequested que ficou guardado, e a corrida entre o varredor e o relay
decide o caminho.

> Mostrar: `contêiner: exited | API pelo nginx: HTTP 502`; `✔ caminho A: o varredor venceu, FAILED relay timeout` ou
> `✔ caminho B: o relay respondeu antes do varredor, chamada ativada`; a linha do tempo dos logs da chamada;
> `relay-sessions: RELEASED`; `a: enviou X, recebeu Y` e `b: …` (≥ 90%); `✔ Falha surpresa: call-signaling caiu e
> voltou; áudio contínuo, chamada pendente resolvida sem efeito duplicado, relay liberado`.

**Conclusão.** O sistema fez o que previmos: a falha ficou contida no call-signaling, o áudio continuou, e a chamada
pendente foi resolvida sem efeito duplicado.

> Fonte: `scripts/demo/surprise-kill-call-signaling.sh`; `ARCHITECTURE.md` §11.1.

---

## Perguntas prováveis do professor

Respostas curtas, para falar. Fonte entre parênteses, para o apresentador.

1. **Por que SNS e SQS, e não RabbitMQ ou Kafka?** Precisávamos de fan-out, consumidores concorrentes, reentrega e DLQ.
   O SNS com uma fila por consumidor e o SQS dão isso nativamente, sem broker para operar. RabbitMQ ou Kafka seriam
   mais um componente para manter, sem ganho para este volume. E o enunciado aceita o serviço gerenciado. (§4, D1, D2)

2. **Por que DynamoDB?** Por dois recursos: escrita condicional, que conta vagas sem lock, e TransactWriteItems, que
   grava efeito, marcador de idempotência e outbox de forma atômica. Um relacional exigiria lock próprio e um servidor.
   (§4, D4, D9)

3. **Como vocês tratam ordenação?** Não dependemos de ordem: SQS padrão não garante, réplicas processam em paralelo e o
   outbox publica sem ordem. Cada efeito confere o estado atual antes de aplicar; a trilha é reordenada por occurredAt
   na leitura. Uma FIFO deduplica só numa janela de cinco minutos e tem menor vazão. O caso não coberto é Joined e Left
   do mesmo usuário invertidos no media-relay; o call-signaling continua correto. (§5.5, D4, §14)

4. **É exatamente uma vez?** A entrega é pelo menos uma vez; exatamente uma vez na entrega não existe no SQS padrão. O
   que garantimos é efeito único: o marcador com o messageId e o efeito vão na mesma transação, então nunca há efeito
   sem marcador nem marcador sem efeito. Uma duplicata falha no marcador e é descartada. (§5.1, D4)

5. **Por que o call-signaling tem uma réplica só?** O hub de SSE fica em memória: uma notificação só chega a quem está
   conectado naquela réplica. Escalar exigiria distribuir as notificações entre réplicas, por exemplo com SNS e uma
   fila por réplica, e um balanceador. O R4 é atendido pelo media-relay e pelo audit-vault. (§6.6, D15, §14)

6. **E se o líder do media-relay cair?** O lease para de ser renovado. Depois de mais de três segundos parado, pelo
   relógio de quem observa, outro pod reivindica com term mais um, numa escrita condicional; só um vence. Se o líder
   antigo voltar de uma pausa, as escritas dele falham no fencing por term, e ele registra leadership lost. (§7.6, D12;
   `MR/internal/app/failover.go:95`)

7. **Por que o áudio é exceção na idempotência?** O S3 não participa de transação do DynamoDB, e o áudio não cabe num
   item de 400 KB. A chave do objeto é determinística e inclui o messageId: uma reentrega sobrescreve o mesmo objeto, e
   o efeito continua único. Se duas réplicas recebem a mesma mensagem, a que perde a corrida do marcador registra
   duplicate ignored. (`ARCHITECTURE.md` §9.4, D19; `AV/AudioProcessor.cs:41-44`)

8. **Como o sistema escala?** O trabalho assíncrono escala adicionando consumidores na mesma fila: o media-relay pela
   variável MEDIA_RELAY_REPLICAS, como no F8. Cada chamada fica no pod que a recebeu, e o failover redistribui pelo
   critério de menos sessões. O audit-vault escala por AUDIT_VAULT_REPLICAS, hoje limitado a duas pela faixa de
   portas. O call-signaling é o limite: uma réplica. (§1.3, §8.5, §14)

9. **Quais são as limitações conhecidas?** Call-signaling com uma réplica; MiniStack só em memória; outbox sem ordem;
   marcadores sem TTL; gravação melhor esforço e só do navegador; depois de um failover, o relayId mostrado é o
   original; e qualquer usuário autenticado entra numa chamada ACTIVE com vaga. (§14)

10. **Por que a oferta WebRTC vai por HTTP, e não por fila?** É pedido e resposta, com o navegador esperando, e precisa
    chegar ao pod dono da chamada. Por fila teríamos latência e correlação de resposta; um balanceador mandaria o áudio
    ao pod errado. É uma das duas únicas chamadas síncronas entre serviços. (D11)

11. **E se o processo morrer entre publicar o outbox e apagar o item?** A mesma mensagem, com o mesmo messageId, é
    publicada de novo na varredura seguinte, e o consumidor a descarta pelo marcador. (§5.2)

12. **Vocês implementaram algum bônus?** Não. O init.sh é script de AWS CLI, não Terraform ou CDK. A API de leitura do
    audit-vault é separada, mas não é CQRS. O rastreio é pelo correlationId nos logs, sem OpenTelemetry. E os eventos
    só têm a versão 1. (`PLAN.md` §1; `txt:96-101`; `COMO-FUNCIONA.md` §1.2)

13. **Por que WebRTC, e não WebSocket?** O navegador só manda áudio por UDP via WebRTC. WebSocket é TCP: um pacote
    perdido segura os seguintes. E o WebRTC já traz criptografia obrigatória, buffer de jitter e o Opus.
    (`COMO-FUNCIONA.md` §6.2, §7.2)

14. **Por que SFU, e não cada navegador falar com os outros?** No mesh, o servidor não vê o áudio: sem gravação, sem
    failover, e com NAT entre pares. O SFU só copia pacotes, então custa pouca CPU e permite gravar. (§7.3)

15. **Por que Go?** Goroutines são baratas: cada serviço tem uma por laço de consumo, uma para o outbox, uma para o
    batimento e uma por faixa de áudio no SFU. O gravador usa select não bloqueante: se a fila enche, descarta o
    trecho em vez de travar o áudio. O C# ficou na auditoria, fora do caminho de tempo real. (§3.1–3.3)

16. **Por que SSE, e não WebSocket, para as notificações?** É só servidor para cliente, sobre HTTP comum, e o navegador
    reconecta sozinho. (D15)

17. **Por que tokens EdDSA, e não HS256?** Só o call-signaling tem a chave privada; quem verifica, como o media-relay,
    só tem a pública e não consegue emitir token. O algoritmo é fixo, e o token de join dura um minuto. (§16.1, D14)

18. **Como o failover sabe que o pod morreu, e não só atrasou?** Dois sinais: sem batimento há mais de três segundos e
    sem resposta no /health. (§7.6, §11.2)

19. **O que se perde da gravação num failover?** Até cinco segundos: o trecho aberto na memória do pod morto. (D20,
    `DEMO-LAN.md` §4)

20. **E se as tabelas crescerem?** Os Scan já leem todas as páginas. O próximo passo seria um índice secundário (GSI)
    para não varrer a tabela. (§12.3, §16.6)

---

## Tabela de tempo

| Slide | Alvo (s) | Palavras | Estimado (s) |
|---|---|---|---|
| 1 Capa | 15 | 33 | 15 |
| 2 Tema e fluxo principal | 45 | 97 | 45 |
| 3 Os três elementos | 45 | 94 | 43 |
| 4 Diagrama | 55 | 119 | 55 |
| 5 Serviços e linguagens | 20 | 44 | 20 |
| 5b Áudio: WebRTC, SFU, Opus | 40 | 88 | 40 |
| 5c Do navegador ao servidor (porta UDP) | 55 | 122 | 56 |
| 6 Nuvem e justificativas | 75 | 161 | 74 |
| 7 Broker e fan-out | 40 | 85 | 39 |
| 8 Banco por serviço | 30 | 67 | 31 |
| 9 Contratos e envelope | 30 | 66 | 30 |
| 10 Contêineres e infra | 45 | 97 | 45 |
| 11 Saga | 50 | 109 | 50 |
| 12 R1 | 50 | 107 | 49 |
| 13 R2 | 35 | 79 | 36 |
| 14 R3 | 25 | 57 | 26 |
| 15 R4 | 50 | 108 | 50 |
| 16 R5 | 20 | 41 | 19 |
| 17 R6 | 35 | 79 | 36 |
| 18 Failover | 45 | 99 | 46 |
| 19 Menu F1–F8 | 30 | 62 | 29 |
| 20 Falha surpresa: previsão | 40 | 90 | 42 |
| 21 Decisões | 30 | 65 | 30 |
| 22 Limitações | 30 | 64 | 30 |
| 23 Encerramento | 15 | 37 | 17 |
| **Total** | **955** | **2.070** | **~954 = 15 min 54 s** |

Fora do total (não contam no tempo): demo ao vivo do cenário escolhido, falha surpresa e perguntas. O F4 leva ~2,5–3 min
de script.
