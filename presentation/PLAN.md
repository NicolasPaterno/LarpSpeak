# Plano da apresentação — Larpspeak

Plano slide a slide para construir o deck HTML da apresentação final de Sistemas Distribuídos (FURB).
Fonte do que precisa ser apresentado: `.ai/assignment/trabalho-final.txt` (citado como `txt:<linha>`).
Fonte do que o sistema faz: `ARCHITECTURE.md` (citado como `§<seção>`), conferido no código.
Convenções de caminho: `CS` = `services/call-signaling`, `MR` = `services/media-relay`, `AV` = `services/audit-vault`.

## Histórico de revisão

- 2026-11-25 — revisão do plano (achados e justificativas em `presentation/REVIEW.md`):
  - Slides 19 ("F1–F8 em uma tela") e 20 ("Demo ao vivo") fundidos no slide 19 "F1–F8 e demo ao vivo": os dois eram
    menus dos mesmos apêndices. O deck passa de 24 para 23 slides; os antigos 21–24 viraram 20–23.
  - Demo ao vivo roda `scripts/demo/fN.sh` (fluxo principal). `make test_caseN` também roda `audit-fN.sh` (F2, F3, F4,
    F6, F8) e dobra o tempo; fica para gravar evidências antes e para quando o professor pedir o audit-vault.
    Durações estimadas por cenário e roteiro próprio para F4 (~2,5–3 min de espera).
  - Orçamento de tempo refeito: 985 s (16 min 25 s). Slide 5: 30 → 20 s; slide 18: 30 → 45 s (sequência reduzida a
    5 passos); menu F1–F8: 45 → 30 s; decisões: 40 → 30 s (recapitulação).
  - Componentes visuais alinhados a `DESIGN.md` §5 (cantos retos, fila com duas barras, S3 e Secrets Manager como
    cilindros, HTTP amber contínua, mensagem blue tracejada); marcadores ✓/✗ e ícones de relógio, dado, ampulheta e
    coroa trocados por selos de texto e pelo ícone `X` de `Icons.tsx`.
  - Seção 5: regras de densidade (`DESIGN.md` 4.4–4.6) e piso de fonte de 24px; o excedente vai para as notas.
  - Slide 9: formato "JSON Schema" explícito (`txt:67`).
  - Slide 17: linhas de log reais (Go `receive failed, retrying` e `outbox scan failed, retrying`; C#
    `broker unreachable, retrying`) e sequência de backoff corrigida (… 2 s, 4 s, 5 s).
  - Apêndices A2, A5 e A8: comando para mostrar o estado do banco (`txt:120`); nome real dos contêineres em A8.
  - Slide 20 (falha surpresa): previsão para `web` e para MiniStack reiniciado nas notas; recuperação por `make start`.
  - Pré-requisitos da demo: `make build` antes, python3 e Go para os scripts do audit-vault, token padrão do
    audit-vault escrito por extenso (`larpspeak-audit-demo`).

---

## 1. Objetivo, público, duração, formato

- **Objetivo**: defender a arquitetura do Larpspeak e provar, apontando o código, que cada exigência do enunciado
  está atendida (R1–R6, F1–F8, regras gerais), e servir de apoio para a demonstração ao vivo da falha escolhida pelo
  professor e de uma falha surpresa.
- **Público**: o professor da disciplina (Prof. Gabriel Castellani de Oliveira, `txt:6`) e a turma.
- **Apresentador**: uma pessoa.
- **Duração**: teto de **20 min** só para a apresentação do projeto (slides 1–23, 5b e 5c): ~955 s (~16 min).
  - Perguntas, demo ao vivo do cenário escolhido (slide 19 + apêndice) e falha surpresa **não** contam no tempo.
- **Formato**: um único arquivo HTML em `presentation/deck/index.html`, slides 16:9, visual conforme
  **ver DESIGN.md** (`presentation/DESIGN.md`: cores, fontes, espaçamentos, componentes). Este plano não define
  cores nem fontes; onde diz "destaque", "atenuado", "erro" ou "sucesso", usar os tokens equivalentes de DESIGN.md.
- **Idioma**: PT-BR. Identificadores, tipos de mensagem, nomes de fila/tabela e linhas de log ficam em inglês, como no
  código.
- **Regra de serviços/linguagens**: o deck apresenta **3 microsserviços em 2 linguagens** (call-signaling Go,
  media-relay Go, audit-vault C#) como atendimento de `txt:12` ("pelo menos três microsserviços escritos em no mínimo
  duas linguagens"). Não citar outra regra de quantidade nem conflito na tela; se perguntado, a resposta está nas notas
  do slide 5. O `web` (nginx + React) aparece nos diagramas
  como cliente/proxy, rotulado "não é microsserviço".
- **Bônus** (`txt:96-101`): nenhum foi implementado. O deck não reivindica nenhum. Só mencionar se perguntado (nota de
  fala no slide 22).

---

## 2. Tabela de cobertura

| Requisito do enunciado | Linha(s) do .txt | Slide(s) |
|---|---|---|
| Tema próprio + fluxo principal | 12, 16, 24 | 2 |
| Transação distribuída com compensação | 20 | 3, 11, A1, A7 |
| Dependência lenta ou instável, simulada | 21 | 3, 16 |
| Evento com vários interessados (publicado uma vez, ≥ 2 consumidores) | 22 | 3, 7 |
| Pelo menos três microsserviços, em no mínimo duas linguagens | 12, 28 | 5 |
| Microsserviços coordenados por eventos | 28 | 4, 7 |
| Diagrama: serviços, bancos, mensageria, serviços de nuvem | 28 | 4 |
| Plano de dados: WebRTC, SFU, Opus (decisão de arquitetura) | 12 | 5b, 5c |
| Gravação cifrada (S3 + Secrets Manager em uso) | 28 | 6 |
| Justificativa de cada serviço de nuvem (por que ele e não alternativa) | 28 | 6 |
| ≥ 3 categorias da tabela, mensageria obrigatória | 30, 32-59 | 6 |
| MiniStack: serviços escolhidos são emulados | 61 | 6, 10 |
| Banco por serviço (ninguém lê/escreve o banco de outro) | 65 | 8 |
| Broker à escolha, justificado | 66 | 7 |
| Contratos explícitos em `contracts/` + envelope único | 67 | 9 |
| Contêineres: Dockerfile + health check + `docker compose up` | 68 | 10 |
| R1 at-least-once + idempotência (apontar no código) | 72, 77-79 | 12, A3 |
| R2 controle de concorrência (zero venda dupla) | 80-82 | 13, A5 |
| R3 API assíncrona (202 + id + consulta de status) | 83-85 | 14 |
| R4 consumidores concorrentes + discussão de ordenação | 86-88 | 15, A8 |
| R5 dependência instável por variável de ambiente | 89-91 | 16, A1, A7 |
| R6 reconexão sem reinício manual | 92-94 | 17, A6 |
| Endpoints, credenciais e nomes de filas por variável de ambiente | 105 | 10 |
| MiniStack: um `docker compose up`, `AWS_ENDPOINT_URL`, init por script | 112-114 | 10 |
| Infraestrutura por script versionado; recriar seguindo o README | 116 | 10 |
| F1 Compensação | 126-129 | 19, A1 |
| F2 Serviço morre no meio | 130-133 | 19, A2 |
| F3 Mensagem duplicada | 134-137 | 19, A3 |
| F4 Mensagem venenosa | 138-141 | 19, A4 |
| F5 Corrida pelo recurso | 142-145 | 19, A5 |
| F6 Broker fora do ar | 146-149 | 19, A6 |
| F7 Resposta tardia | 150-153 | 19, A7 |
| F8 Escala horizontal | 154-157 | 19, A8 |
| Professor escolhe uma falha; provocar ao vivo; mostrar logs + estado dos bancos | 120 | 19, A1–A8 |
| Falha surpresa: prever antes de executar | 159 | 20 |
| Decisões de arquitetura (a nota está nelas) | 12 | 18, 21 |
| Limitações conhecidas | (honestidade; `§14`) | 22 |
| Bônus | 96-101 | nenhum implementado; nota de fala no slide 22 |
| Encerramento | — | 23 |

---

## 3. Roteiro de slides

Orçamento de tempo (segundos):

| # | Slide | s |
|---|---|---|
| 1 | Capa | 15 |
| 2 | Tema e fluxo principal | 45 |
| 3 | Os três elementos exigidos | 45 |
| 4 | Diagrama da arquitetura | 55 |
| 5 | Serviços e linguagens | 20 |
| 5b | Áudio: WebRTC, SFU e Opus | 40 |
| 5c | Do navegador ao servidor: oferta WebRTC e porta UDP | 55 |
| 6 | Serviços de nuvem e justificativas (com a gravação cifrada) | 75 |
| 7 | Broker: SNS + SQS e fan-out | 40 |
| 8 | Banco por serviço | 30 |
| 9 | Contratos e envelope | 30 |
| 10 | Contêineres, infraestrutura por script, MiniStack | 45 |
| 11 | Saga com compensação | 50 |
| 12 | R1 idempotência + outbox | 50 |
| 13 | R2 concorrência | 35 |
| 14 | R3 API assíncrona | 25 |
| 15 | R4 consumidores concorrentes e ordenação | 50 |
| 16 | R5 dependência instável | 20 |
| 17 | R6 reconexão | 35 |
| 18 | Failover do media-relay | 45 |
| 19 | F1–F8 e demo ao vivo (30 s de menu; a demo não conta no tempo) | 30 |
| 20 | Falha surpresa: previsão | 40 |
| 21 | Decisões de arquitetura (recapitulação) | 30 |
| 22 | Limitações conhecidas | 30 |
| 23 | Encerramento | 15 |
| | **Total** | **955 s ≈ 16 min** |

Conferência de ritmo: 955 s de fala para 25 slides (média de 38 s, ~83 palavras por slide a 130 palavras/min).
Slides com mais de 3 itens na tela levam o excedente para as notas (seção 5).

Slides de apêndice A1–A8 (um por cenário F1–F8) ficam depois do 23 e só são abertos para o cenário que o professor
escolher. O tempo deles não conta nos 20 min.

### Componentes visuais recorrentes (usar em todos os diagramas)

Definir uma vez no HTML e reaproveitar. Formas, traços e cores seguem `DESIGN.md` §5.2–5.5; este bloco só lista o
que cada diagrama usa.

- **Serviço**: retângulo de cantos retos com o nome (`call-signaling`) e selos de linguagem e réplicas
  (`GO 1.27`, `C# · .NET 10`, `×1`, `×2`).
- **Tabela DynamoDB**: cilindro com eyebrow `DYNAMODB` e o prefixo (`signaling-*`, `relay-*`, `audit-*`).
- **Fila SQS**: retângulo com duas barras verticais internas, eyebrow `SQS` e o nome. **DLQ**: mesma forma, traço red
  tracejado, nome `<fila>-dlq`.
- **Tópico SNS**: hexágono achatado com eyebrow `SNS` e o nome `call-events`.
- **S3** e **Secrets Manager**: cilindros iguais ao do DynamoDB, com eyebrow `S3` / `SECRETS MANAGER` e o nome
  (`larpspeak-audio`, `larpspeak/jwt-key`, `larpspeak/audio-key`).
- **Cliente**: pílula (`Navegador`, `voice-client`, `operador / curl`).
- **web · nginx**: retângulo tracejado sem preenchimento, selo `NGINX 1.29`, rótulo "não é microsserviço".
- **Seta assíncrona** (mensagem SQS/SNS): blue, tracejada `10 6`, rótulo = tipo(s) da mensagem.
- **Seta síncrona** (HTTP, inclusive SSE): amber, contínua, rótulo = rota.
- **Mídia** (áudio UDP/WebRTC): signal, pontilhada `1 8`, ponta nas duas extremidades.
- **Acesso a banco**: dim, contínua, sem ponta.
- **Destaque**: fatia de `DESIGN.md` 5.5. Foco em signal (foco de falha em red); o resto do diagrama em opacidade 0.4.
  Zoom = o mesmo SVG com outro `viewBox`.
- **Marcadores**: neste plano, "✓" e "✗" são só notação. Desenhar sucesso como selo signal com a palavra (`OK`,
  `ACTIVE`, `200`) e falha como o ícone `X` de `Icons.tsx` em red com a palavra (`FALHA`, `409`, `DLQ`). Onde o plano
  cita relógio, dado, ampulheta ou coroa, usar selo de texto (`200 ms`, `100%`, `30 s`, `LÍDER · term 7`): não há
  esses ícones em `Icons.tsx`, e `DESIGN.md` §8 proíbe ícones de fora e emoji. "Seta verde" = signal; "seta vermelha"
  = red.

O "diagrama base" é o do slide 4. Os recortes dos slides seguintes são subconjuntos dele, com as mesmas posições
relativas sempre que possível (call-signaling à esquerda, media-relay ao centro/direita, audit-vault à direita,
mensageria no meio, bancos embaixo de cada serviço, nuvem auxiliar no canto inferior direito).

---

### Slide 1 — Capa (15 s)

- **Objetivo**: identificar o trabalho.
- **Conteúdo**:
  - Larpspeak — streaming e orquestração de voz em tempo real
  - Sistemas Distribuídos · FURB
  - Nícolas Paterno · William Barboza · Gustavo Amaro · Gustavo Luchini
  - (só grupo, disciplina e nome do projeto: decisão do responsável)
- **Ilustração**: miniatura do diagrama base (slide 4) só com os três serviços e o hexágono `call-events` ligados,
  sem rótulos, como marca visual.
- **Fonte**: `txt:2-6`, `txt:162`; `README.md` (primeira linha).

### Slide 2 — Tema e fluxo principal (45 s)

- **Objetivo**: dizer o que o sistema faz e qual é o fluxo avaliado.
- **Conteúdo**:
  - Backend de chamadas de voz em grupo (estilo TeamSpeak/Discord)
  - Fluxo principal: **criar chamada → entrar → falar**
  - Áudio direto do cliente ao relay (WebRTC/UDP); controle por eventos
  - Regra de negócio simples; foco em falhas
- **Ilustração**: linha do tempo horizontal com 3 passos numerados:
  1. `POST /calls` → `202 {callId, PENDING}`;
  2. `RelayAllocated` → chamada `ACTIVE` → SSE `call_active`;
  3. `POST /calls/{id}/join` + oferta WebRTC → áudio (seta pontilhada) `Navegador ⇢ media-relay`.
  Abaixo de cada passo, o serviço responsável (call-signaling, media-relay, media-relay).
- **Fonte**: `§1`, `§8.1`, `§8.3`; `README.md`.
- **Nota de fala**: tema registrado com o professor: "Streaming e orquestração de voz/áudio". No deck:
  "streaming e orquestração de voz em tempo real".

### Slide 3 — Os três elementos exigidos (45 s)

- **Objetivo**: mostrar onde estão compensação, dependência instável e evento com vários interessados.
- **Conteúdo** (3 colunas, uma por elemento):
  - **Compensação**: criação da chamada CS ↔ MR; falha ou timeout → `FAILED`, vagas e usuário liberados, cliente
    avisado (`call_failed`)
  - **Dependência instável**: alocação do relay; `RELAY_LATENCY_MS`, `RELAY_FAILURE_RATE`
  - **Evento com vários interessados**: `CallStarted`, `ParticipantJoined`, `ParticipantLeft`, `CallEnded`,
    `CallFailed` → media-relay **e** audit-vault
- **Ilustração**: três mini-recortes lado a lado, cada um com a cor de destaque:
  1. `call-signaling` ⇄ `media-relay` com setas `RelayRequested` (→) e `RelayFailed` (←), um ✗ sobre o
     `media-relay` e um laço de volta em `call-signaling` rotulado `compensate`.
  2. `media-relay` com dois selos de texto (`LATÊNCIA`, `FALHA %`) e os rótulos `RELAY_LATENCY_MS`,
     `RELAY_FAILURE_RATE`.
  3. `call-signaling` → hexágono `SNS call-events` → duas setas para `media-relay` e `audit-vault`.
- **Fonte**: `§1.2`; `CS/internal/app/saga.go`; `MR/internal/app/relay.go` (`allocate`, linha 176).

### Slide 4 — Diagrama da arquitetura (55 s)

- **Objetivo**: o diagrama exigido por `txt:28` (serviços, bancos, mensageria, serviços de nuvem).
- **Conteúdo** (legenda curta ao lado):
  - 3 microsserviços, coordenados por eventos
  - Mensageria: SNS + SQS
  - Nuvem: DynamoDB, S3, Secrets Manager (MiniStack)
  - Só 2 chamadas HTTP entre serviços
- **Ilustração** (diagrama base, deve bater com `§1.1` exatamente):
  - Clientes à esquerda: `Navegador`, `voice-client`, `operador / curl`.
  - `web · nginx` (rotulado "proxy, não é microsserviço") entre `Navegador` e `call-signaling`, seta HTTP (amber
    contínua) `HTTP /api/* + SSE`.
  - `voice-client` → `call-signaling`: HTTP contínua `HTTP + SSE (:8081)`.
  - `operador / curl` → `audit-vault`: HTTP contínua `GET /audit/* Bearer`.
  - Serviços: `call-signaling · Go · ×1`, `media-relay · Go · ×2`, `audit-vault · C# · ×2`.
  - Mídia (pontilhada dupla-ponta): `Navegador ⇢ media-relay` `WebRTC Opus`; `voice-client ⇢ media-relay` `UDP PCM`.
  - HTTP entre serviços (amber contínuas, como no componente "Seta HTTP"): `call-signaling → media-relay` `POST /webrtc`;
    `media-relay → call-signaling` `GET /auth/public-key`.
  - Bancos (cilindros embaixo de cada serviço): `DynamoDB signaling-*`, `DynamoDB relay-*`, `DynamoDB audit-*`.
  - Mensageria no centro:
    - `call-signaling → [SQS relay-requests] → media-relay`, rótulo `RelayRequested / ReleaseRelayRequested`;
    - `media-relay → [SQS relay-results] → call-signaling`, rótulo
      `RelayAllocated / RelayFailed / RelayMoved / ParticipantTimedOut`;
    - `call-signaling → {SNS call-events}` → `[SQS media-relay-call-events] → media-relay` e
      `[SQS audit-vault-call-events] → audit-vault`;
    - `media-relay → [SQS audio-chunks] → audit-vault`, rótulo `AudioChunk`.
  - Nuvem auxiliar: `audit-vault → S3 larpspeak-audio` (rótulo `AES-256-GCM`); `call-signaling → Secrets Manager
    larpspeak/jwt-key`; `audit-vault → Secrets Manager larpspeak/audio-key`.
  - Moldura pontilhada em volta de toda a parte AWS, rotulada `MiniStack :4566 (AWS emulada)`.
  - Construção progressiva opcional (fragmentos ao avançar): serviços → bancos → mensageria → nuvem auxiliar.
- **Fonte**: `§1.1`, `§1.3`; `docker-compose.yml`; `infra/init/init.sh`.

### Slide 5 — Serviços e linguagens (20 s)

- **Objetivo**: atender `txt:12` e mostrar a organização do código.
- **Conteúdo**:
  - 3 microsserviços · 2 linguagens
  - `call-signaling` Go 1.27 — API, contas/JWT, saga, vagas, SSE
  - `media-relay` Go 1.27 — aloca relay, encaminha áudio, failover, grava trechos
  - `audit-vault` C# / .NET 10 — trilha imutável, gravações cifradas
  - Go em portas e adaptadores; nenhum código compartilhado, só `contracts/`
- **Ilustração**: três cartões de serviço (componente "Serviço") lado a lado. Dentro de cada cartão Go, um
  hexágono em camadas: `domain` (centro) ← `app` (`ports.go`) ← `adapter/*` (borda) com os adaptadores listados
  (CS: `dynamo, messaging, httpapi, sse, relayhttp, secrets`; MR: `dynamo, messaging, httpapi, udp, webrtc, pubkey`).
  No cartão C#: lista das interfaces-porta `IMessageQueue, IAuditStore, IAudioStore, IAudioKeyProvider,
  IProcessedStore, IMessageHandler`. Uma faixa embaixo dos três: `contracts/` (único ponto comum).
- **Fonte**: `§1`, `§9.1`, `§12.1`, D16; `CS/main.go`, `MR/main.go`, `AV/Program.cs`.
- **Nota de fala**: por que Go (goroutines por laço de consumo, outbox, batimento e faixa de áudio; `select` não
  bloqueante no gravador) e por que C# (auditoria fora do caminho de tempo real). Se perguntarem "três linguagens":
  `txt:12` diz duas; as Regras (`txt:64`) dizem três; seguimos a introdução. `COMO-FUNCIONA.md` §3.1–3.3.

### Slide 5b — Áudio: WebRTC, SFU e Opus (40 s)

- **Objetivo**: explicar o plano de dados (o áudio), que nenhum outro slide cobre.
- **Conteúdo**:
  - WebRTC: único UDP do navegador; DTLS-SRTP obrigatório
  - SFU: copia pacotes sem decodificar → grava e faz failover
  - Opus: quadros de 20 ms, FEC; não retransmite, não cancela eco (é o navegador)
- **Ilustração**: três navegadores ligados a um `media-relay · SFU` central; cada um manda 1 fluxo e recebe N−1.
  Ao lado, mini-tabela 3 × 3 "mesh · MCU · SFU" com as linhas "CPU no servidor", "grava?", "failover". Selo `Opus
  48 kHz · 20 ms · FEC` sobre as setas de mídia (pontilhadas).
- **Fonte**: `COMO-FUNCIONA.md` §6.2, §7.2–7.4; `MR/internal/adapter/webrtc/sfu.go:1-4, 90-97`.
- **Nota de fala**: DTX desligado de propósito (presença depende de pacotes, D13); mesh sem gravação e com NAT entre
  pares; MCU decodifica, mixa e recodifica.

### Slide 5c — Do navegador ao servidor: oferta WebRTC e porta UDP (55 s)

- **Objetivo**: mostrar como o navegador manda o áudio, como o servidor recebe e por que a porta UDP é publicada.
- **Conteúdo**:
  - Oferta SDP por HTTP: navegador → call-signaling → pod dono (token `join` de 1 min)
  - Resposta com 1 candidato: `RELAY_HOST:50000/udp`
  - Áudio direto ao pod: ICE → DTLS → SRTP/Opus a cada 20 ms; porta publicada pelo `make start`
- **Ilustração**: sequência em 3 faixas. Faixa de sinalização (seta HTTP amber contínua): `Navegador` →
  `web · nginx` → `call-signaling` → `media-relay (pod dono)` `POST /webrtc`, e a resposta `{sdp}` voltando.
  Faixa de mídia (pontilhada): `Navegador ⇢ IP da máquina :50000/udp ⇢ [moldura Docker] ⇢ media-relay :50000`,
  com selos `ICE`, `DTLS`, `SRTP · Opus 20 ms`. Dentro da moldura Docker, os dois pods do modo voz:
  `media-relay` (40000/50000) e `media-relay-2` (40001/50001). Rodapé: `docker compose up` puro = sem porta UDP
  publicada (✗ no caminho de mídia).
- **Fonte**: `COMO-FUNCIONA.md` §7.5; `web/src/voice.ts:96-123`; `CS/internal/app/saga.go:320-332`;
  `MR/internal/adapter/httpapi/health.go:26-57`; `MR/internal/adapter/webrtc/sfu.go:102-107`; `compose.voice.yml`.
- **Nota de fala**: uma porta por pod porque todos os peers do pod dividem um socket (`SetICEUDPMux`); cada pod a
  mais no modo voz é um serviço com porta própria (`media-relay-3`, …); sem STUN/TURN nem trickle; limitação: só
  rede local ou IP público direto. Autoescala é bônus (`txt:99`), não implementada.

### Slide 6 — Serviços de nuvem e justificativas (55 s)

- **Objetivo**: ≥ 3 categorias (`txt:30`) e "por que ele, e não uma alternativa" (`txt:28`).
- **Conteúdo** (tabela de 4 linhas: categoria · serviço · por quê · alternativa descartada):
  - Mensageria · SNS + SQS · fan-out, consumidores concorrentes, reentrega, DLQ nativa, sem broker para operar ·
    RabbitMQ/Kafka em contêiner
  - Banco · DynamoDB · escrita condicional sem lock (R2), `TransactWriteItems` atômica (R1) · relacional com lock
    próprio e servidor
  - Objetos · S3 · áudio binário e grande; item DynamoDB ≤ 400 KB · áudio no DynamoDB
  - Gravação (faixa abaixo da tabela): `media-relay` → trechos de 5 s → `audio-chunks` → `audit-vault` →
    AES-256-GCM com AAD `callId+userId+início` → S3 (`LSA1|nonce|tag|ciphertext`); chave em `larpspeak/audio-key`
  - Segredos · Secrets Manager · chave JWT e chave AES fora do código e do compose · variáveis de ambiente (vazam em
    `docker inspect`)
  - Rodapé: 4 categorias (mínimo 3); todos emulados pelo MiniStack 1.5.40
- **Ilustração**: os quatro ícones de nuvem (fila+hexágono, cilindro, balde, cadeado) dentro da moldura
  `MiniStack :4566`, cada um com uma seta fina para os serviços que o usam:
  SNS/SQS ↔ os 3 serviços; DynamoDB ↔ os 3; S3 ← só audit-vault; Secrets Manager ← call-signaling (`jwt-key`) e
  audit-vault (`audio-key`).
- **Fonte**: `§4`; `infra/init/init.sh:43-63`; `COMO-FUNCIONA.md` §8.1, §8.4.
- **Nota de demo**: prova ao vivo, só se pedirem: `make recording` (`LSA1`, 401 sem token, 500 `recording failed
  authentication` com trecho trocado de pasta, um `.ogg` por participante; `DEMO-LAN.md` §4).

### Slide 7 — Broker: SNS + SQS e fan-out (40 s)

- **Objetivo**: justificar o broker (`txt:66`) e mostrar o evento com vários interessados (`txt:22`).
- **Conteúdo**:
  - Fatos → tópico `call-events`, **uma fila por consumidor** (D1)
  - Comandos/respostas → filas ponto a ponto (D2)
  - Consumidor fora do ar não afeta os outros
  - Cada fila: DLQ, `maxReceiveCount=5`, visibility 30 s
- **Ilustração** (recorte só do fan-out): `call-signaling` (destaque) → hexágono `SNS call-events` → duas filas
  `media-relay-call-events` e `audit-vault-call-events` (`RawMessageDelivery=true`) → `media-relay` (efeito:
  `Release`/`Member`) e `audit-vault` (efeito: trilha). Cada fila com uma caixinha `-dlq` pendurada embaixo.
  À parte, atenuado, o par ponto a ponto `relay-requests` / `relay-results` entre call-signaling e media-relay,
  rotulado "comando / resposta".
- **Fonte**: `§2.1`, `§2.3`, D1, D2; `infra/init/init.sh:6-36`; `CS/internal/adapter/messaging/bus.go:140-151`.

### Slide 8 — Banco por serviço (30 s)

- **Objetivo**: `txt:65`.
- **Conteúdo**:
  - Cada serviço só acessa as próprias tabelas
  - Nomes das tabelas por variável de ambiente
  - Informação entre serviços só por mensagem
- **Ilustração**: três colunas (um serviço no topo de cada), cada uma com seus cilindros:
  - call-signaling: `signaling-calls`, `signaling-users`, `signaling-outbox`, `signaling-processed`,
    `signaling-accounts`, `signaling-refresh`;
  - media-relay: `relay-sessions`, `relay-nodes`, `relay-outbox`, `relay-processed`;
  - audit-vault: `audit-records`, `audit-processed`, `S3 larpspeak-audio`.
  Entre as colunas, um "✗" sobre uma seta tracejada cruzando de um serviço para a tabela do vizinho (acesso proibido),
  e uma seta de mensagem válida passando por um ícone de fila.
- **Fonte**: `§3`; `docker-compose.yml:46-51, 88-91, 105-106`.

### Slide 9 — Contratos e envelope (30 s)

- **Objetivo**: `txt:67`.
- **Conteúdo**:
  - Contratos em **JSON Schema**: `contracts/envelope.schema.json` + 12 schemas em `contracts/events/*.v1.schema.json`
  - Campos: `messageId`, `correlationId` (= `callId`), `type`, `version`, `occurredAt`, `payload`
  - Envelope inválido = mensagem venenosa → DLQ
- **Ilustração**: bloco de código JSON de um envelope real `RelayRequested` com os seis campos, e setas de anotação:
  `messageId` → "chave de idempotência (R1)"; `correlationId` → "rastreio da chamada"; `type`/`version` →
  "roteamento e evolução". Ao lado, lista compacta dos 12 tipos agrupada por canal (`relay-requests`: 2;
  `relay-results`: 4; `call-events`: 5; `audio-chunks`: 1).
- **Fonte**: `§2.2`, `§2.3`, D3; `contracts/`; `CS/internal/domain/envelope.go:21-52`; `AV/Envelope.cs:19-52`.

### Slide 10 — Contêineres, infraestrutura por script, MiniStack (45 s)

- **Objetivo**: `txt:68`, `txt:105`, `txt:112-116`.
- **Conteúdo**:
  - Dockerfile + `GET /health` em cada serviço
  - `docker compose up --build` sobe tudo
  - `infra/init/init.sh` idempotente cria filas, DLQs, tópico, tabelas, bucket, segredos
  - `AWS_ENDPOINT_URL=http://ministack:4566`; filas/tabelas/segredos por env
  - Recriar do zero: `make stop && make start` (README); sem `make`: `docker compose down -v && docker compose up --build`
- **Ilustração**: diagrama de ordem de partida do compose (esquerda → direita):
  `ministack` (healthcheck `/_ministack/health`) → `infra-init` (`aws-cli` roda `init.sh`, termina) →
  `call-signaling ×1`, `media-relay ×2`, `audit-vault ×2` (cada um com um selo "♥ /health"), com a seta rotulada
  `depends_on: service_completed_successfully`; `web` depois de `call-signaling` (`docker-compose.yml:63`). Embaixo, um trecho de env do compose:
  `AWS_ENDPOINT_URL`, `RELAY_REQUESTS_QUEUE`, `CALLS_TABLE`, `JWT_KEY_SECRET`.
- **Fonte**: `§1.3`, D26; `docker-compose.yml:3, 8-13`; `services/*/Dockerfile`; `infra/init/init.sh`; `README.md`.

### Slide 11 — Saga com compensação (50 s)

- **Objetivo**: detalhar a transação distribuída com compensação (`txt:20`).
- **Conteúdo**:
  - call-signaling orquestra (D8)
  - `PENDING → ACTIVE` com `RelayAllocated`
  - `PENDING → FAILED` com `RelayFailed` ou timeout (`RELAY_TIMEOUT_MS` = 3000)
  - Compensação: `taken=0`, usuário liberado, `call_failed`, `CallFailed`
  - Resposta atrasada → `ReleaseRelayRequested`
- **Ilustração** (recorte: só call-signaling, as duas filas ponto a ponto e media-relay):
  - À esquerda, máquina de estados: `PENDING` → `ACTIVE` (seta verde, `RelayAllocated`); `PENDING` → `FAILED`
    (seta vermelha, `RelayFailed` | `SweepTimeouts`); `ACTIVE` → `ENDED` (último sai); `ACTIVE` → `ACTIVE`
    (`RelayMoved`).
  - À direita, sequência: `call-signaling —RelayRequested→ [relay-requests] → media-relay`;
    `media-relay —RelayFailed→ [relay-results] → call-signaling` (✗); `call-signaling` faz `Fail` e publica
    `CallFailed` no SNS (seta curta para o hexágono, atenuado). Ramo pontilhado: `RelayAllocated` atrasado →
    `call-signaling` → `ReleaseRelayRequested` → `media-relay` (`RELEASED, late allocation`).
- **Fonte**: `§6.4`, `§8.2`, D8; `CS/internal/app/saga.go` (`compensate` 159-171, `SweepTimeouts` 375-397,
  resposta atrasada 140-150).

### Slide 12 — R1: entrega pelo menos uma vez + idempotência (50 s)

- **Objetivo**: `txt:77-79`, apontando o código.
- **Conteúdo**:
  - SQS entrega ≥ 1 vez; o **efeito** é idempotente
  - Uma `TransactWriteItems`: marcador `processed/{messageId}` (`attribute_not_exists`) + efeito + outbox
  - Duplicata → falha no item 0 → `duplicate ignored`
  - Outbox: estado e mensagem na mesma transação
  - Código: CS `adapter/dynamo/store.go` (`commit`, `markProcessed`); MR `adapter/dynamo/sessions.go` (`commit`);
    AV `DynamoAuditStore.cs` (`RecordAsync`)
- **Ilustração**: caixa "TransactWriteItems" com três linhas empilhadas:
  `0 Put *-processed {pk: messageId} attribute_not_exists` (destaque), `1 Update efeito (ex.: call ACTIVE)`,
  `2 Put *-outbox (próxima mensagem)`. À esquerda, duas setas entrando com o mesmo `messageId`: a primeira → ✓
  "commit"; a segunda → ✗ no item 0 → "duplicate ignored + delete". À direita, `RelayOutbox` (goroutine,
  a cada 200 ms) lendo `*-outbox` → publica SNS/SQS → apaga o item.
  Nota pequena: áudio usa chave S3 determinística em vez de transação (D19); a réplica que perde a corrida do
  marcador loga `duplicate ignored` (`AV/AudioProcessor.cs:41-44`).
- **Fonte**: `§5.1`, `§5.2`, `§10`, D4, D5; `CS/internal/adapter/dynamo/store.go:393` (`commit`); arquivos acima; `CS/internal/adapter/messaging/bus.go:114-138`.

### Slide 13 — R2: controle de concorrência (35 s)

- **Objetivo**: `txt:80-82` (zero venda dupla).
- **Conteúdo**:
  - Recurso escasso: **vagas da chamada**
  - `Join` = Update condicional `status = ACTIVE AND taken < capacity AND NOT contains(participants, :uid)`
  - Mesma transação: reserva em `signaling-users` (`attribute_not_exists`) — uma chamada por usuário
  - Sem lock, sem coordenador
  - Código: CS `adapter/dynamo/store.go` `Join` (182) / `Leave` (194); condição em `join()` (300)
- **Ilustração**: 5 setas paralelas `POST /join` chegando num único cilindro `signaling-calls` com o contador
  `taken 10/11` (números do F5: criador + 10 vagas); uma seta ✓ (`200`), as outras ✗ (`409 call is full`), com o rótulo
  `ConditionalCheckFailed` sobre as recusadas.
- **Fonte**: `§6.5`, D9, D10; `CS/internal/adapter/dynamo/store.go:182-205, 300-311`.

### Slide 14 — R3: API assíncrona (25 s)

- **Objetivo**: `txt:83-85`.
- **Conteúdo**:
  - `POST /calls` → **202** `{callId, status: "PENDING", statusUrl}` + `Location`
  - Status: `GET /calls/{id}` ou SSE `call_active` / `call_failed`
  - Código: CS `adapter/httpapi/handlers.go` `handleCreate` (linha 46; 202 na linha 82)
- **Ilustração**: diagrama de sequência mínimo com 2 participantes (`Cliente`, `call-signaling`):
  `POST /calls` → `202 {callId, PENDING}` (imediato); intervalo "assíncrono" sombreado; `GET /calls/{id}` →
  `200 {status: ACTIVE}`; em paralelo, seta tracejada SSE `call_active`.
- **Fonte**: `§6.2`, `§10`; `CS/internal/adapter/httpapi/handlers.go:46-82`.

### Slide 15 — R4: consumidores concorrentes e ordenação (50 s)

- **Objetivo**: `txt:86-88`, incluindo a discussão de ordenação exigida.
- **Conteúdo**:
  - media-relay ×2 em `relay-requests`; audit-vault ×2 em `audit-vault-call-events` e `audio-chunks`
  - SQS distribui (pull); sem balanceador
  - **Sem ordem garantida**: SQS padrão, réplicas em paralelo, outbox sem ordem
  - Em vez de ordenar: cada efeito confere o estado atual
    - `Activate`/`Fail` só se `PENDING`
    - `Release`/`Member` só se `ALLOCATED`
    - trilha reordenada por `occurredAt` na leitura
  - Caso não coberto: `ParticipantJoined`/`ParticipantLeft` do mesmo usuário invertidos no media-relay
  - Código: `docker-compose.yml` `deploy.replicas` (75, 99)
- **Ilustração**: fila `relay-requests` com mensagens numeradas `m1 m2 m3 m4` saindo para `media-relay #1` e
  `media-relay #2` (setas alternadas). Embaixo, um exemplo fora de ordem: `CallEnded` chega antes de
  `ParticipantJoined` → `relay-sessions state=RELEASED` → `ParticipantJoined` encontra condição falsa → descartado ✓.
- **Fonte**: `§5.5`, `§8.5`, `§10`; `docker-compose.yml:75, 99`; `AV/AuditModels.cs` (`CallTrail.Build`).

### Slide 16 — R5: dependência instável simulada (20 s)

- **Objetivo**: `txt:89-91`.
- **Conteúdo**:
  - `RELAY_LATENCY_MS` (200 no compose) e `RELAY_FAILURE_RATE` (0 a 1)
  - Na alocação do relay, antes da transação
  - Código: MR `internal/app/relay.go` `allocate` (linha 176)
- **Ilustração**: recorte só do `media-relay` recebendo `RelayRequested`, com um "mostrador" de latência e um
  "dado" de falha; saídas: ✓ `RelayAllocated` e ✗ `RelayFailed {reason: "simulated relay failure"}`.
  Trecho do compose: `RELAY_LATENCY_MS: ${RELAY_LATENCY_MS:-200}`, `RELAY_FAILURE_RATE: ${RELAY_FAILURE_RATE:-0.0}`.
- **Fonte**: `§7.2`, `§10`; `docker-compose.yml:79-80`; `MR/internal/app/relay.go:176-219`.

### Slide 17 — R6: reconexão (35 s)

- **Objetivo**: `txt:92-94`.
- **Conteúdo**:
  - Tentativas no laço, não no SDK (D7)
  - Backoff 250 ms → 5 s; timeout HTTP 15 s > long poll 10 s
  - Logs: Go `receive failed, retrying` (consumidor) e `outbox scan failed, retrying` (outbox); C#
    `broker unreachable, retrying`
  - URL de fila e segredos resolvidos sob demanda
  - Outbox drena sozinho na volta
  - Código: CS/MR `adapter/messaging/bus.go` (`Consume`), `main.go`; AV `QueueConsumer.cs` (`RunAsync`),
    `Program.cs`
- **Ilustração**: linha do tempo horizontal: `ministack` ativo → bloco cinza "pausado 30 s" → ativo.
  Sobre o bloco cinza, os três serviços repetindo `retry` com intervalos crescentes (250 ms, 500 ms, 1 s, 2 s,
  4 s, 5 s, 5 s…). Depois do bloco: setas ✓ "consome de novo", "outbox drena".
- **Fonte**: `§5.3`, `§10`, D7; `CS/internal/adapter/messaging/bus.go` (`backoff`, `Consume` 72-110, `RelayOutbox`
  114-138); `AV/QueueConsumer.cs:67`. Nota: `broker unreachable, retrying` no Go só aparece se a URL da fila ainda não
  foi resolvida (`bus.go:79`); com a URL em cache, a queda aparece como `receive failed, retrying` (`bus.go:87`).

### Slide 18 — Failover do media-relay (45 s)

- **Objetivo**: decisão de arquitetura mais distribuída do projeto (eleição de líder + fencing).
- **Conteúdo**:
  - Batimento a cada 1 s em `relay-nodes`
  - Líder por lease (`pk = leader`, `term`); só ele declara morte
  - Morto = sem batimento > 3 s **e** sem `/health`
  - Move a chamada para o pod com menos sessões; fence por `term`
  - Cliente recebe `relay_moved` e refaz a oferta
- **Ilustração** (recorte do failover, conforme o diagrama de sequência de `§7.6`): três pods `pod A (dono, ✗)`,
  `pod líder`, `pod N (novo dono)`, cilindro `relay-nodes`/`relay-sessions`, fila `relay-results`,
  `call-signaling`, `Navegador`. Cinco setas numeradas (o detalhe fica nas notas):
  1 `beat` parado > 3 s e `GET /health` sem resposta; 2 `MarkDead(A)` + `Move(call → N)` com fence `term`;
  3 `RelayMoved` (outbox → `relay-results`) → call-signaling; 4 SSE `relay_moved` → navegador;
  5 nova oferta `POST /webrtc` → pod N e áudio pontilhado Navegador ⇢ pod N.
- **Fonte**: `§7.6`, D12; `MR/internal/app/failover.go`; `MR/internal/adapter/dynamo/nodes.go`
  (`Claim` 56, `MarkDead` 94, `Move` 118).

### Slide 19 — F1–F8 e demo ao vivo (30 s; demo fora do tempo)

- **Objetivo**: mostrar que todos os cenários têm comando, resultado esperado e evidência, e executar ao vivo o que o
  professor escolher (`txt:120`).
- **Conteúdo na tela** (grade 2 × 4 de cartões, um por cenário; cada cartão: `FN` + nome, provocação em uma linha,
  resultado em uma linha; evidências detalhadas ficam no apêndice AN):
  - F1 · `RELAY_FAILURE_RATE=1.0` · `FAILED`, usuário liberado, `call_failed`
  - F2 · `docker kill` na réplica que logou `allocating relay` · `ACTIVE`, 1 alocação
  - F3 · mesmo `RelayAllocated` 5× · 1 efeito, 4 `duplicate ignored`
  - F4 · texto inválido em `relay-results` · DLQ após 5 recebimentos, resto segue
  - F5 · 100 `join` em 10 vagas · 10 × 200, 90 × 409
  - F6 · `docker compose pause ministack` 30 s · reconecta, outbox drena
  - F7 · `RELAY_LATENCY_MS=6000` > timeout 3000 · `FAILED`, relay `RELEASED` (`late allocation`)
  - F8 · 3 réplicas, 30 chamadas · divisão por réplica, sem duplicata
  - Rodapé: ao vivo `RELAY_HOST=127.0.0.1 scripts/demo/fN.sh`; evidência completa (com audit-vault)
    `make test_caseN`
- **Ilustração**: cada cartão traz a miniatura do recorte do apêndice correspondente (A1–A8) e é um link para ele;
  a grade é o menu da demo. O cartão aponta o ponto de injeção: F1/F7 no `media-relay` (env), F2/F8 nas réplicas do
  `media-relay`, F3/F4 na fila `relay-results`, F5 no cilindro `signaling-calls`, F6 na moldura `MiniStack`.
- **Comandos da demo** (painel de terminal, `DESIGN.md` 4.7; ficam nas notas e no apêndice):
  - Antes da apresentação: `make build` (as imagens; `make start` não reconstrói) e `RELAY_HOST=127.0.0.1 make start`.
  - Rodar ao vivo: `RELAY_HOST=127.0.0.1 scripts/demo/fN.sh`. F1 e F7 já conferem a trilha do audit-vault
    (`audit_failed` em `lib.sh`).
  - Logs: `make logs SERVICE=<serviço>`.
  - Estado: `scripts/aws.sh dynamodb get-item --table-name <tabela> --key '{"pk":{"S":"<id>"}}'`;
    `scripts/aws.sh dynamodb scan --table-name <tabela> --select COUNT`; filas com `scripts/aws.sh sqs ...`.
  - Trilha: `curl -s -H "Authorization: Bearer larpspeak-audit-demo" localhost:8083/audit/calls/<callId> | jq`
    (token padrão do compose, `docker-compose.yml:108`; a variável `AUDIT_API_TOKEN` não existe no shell do host).
- **Notas de demo**:
  - Ordem: o professor escolhe → abrir o slide AN → iniciar o comando → explicar o slide enquanto roda → mostrar a
    linha `✔` final do script, a linha de log e o estado do banco indicados no slide AN.
  - Durações aproximadas de `fN.sh` (não contam no tempo): F1 ~30 s; F2 ~60–75 s; F3 ~30 s; F4 ~2,5–3 min; F5 ~30 s;
    F6 ~90 s; F7 ~40 s; F8 ~40 s.
  - Saída ao vivo: cada script imprime linhas com hora `[HH:MM:SS.mmm]`, as linhas reais de log (`│ hora contêiner
    mensagem`) e termina num bloco `== Evidência` (`banco:`, `log:`) e na linha `✔`. Cada `✔` sai de uma checagem; se
    falhar, o script para com `✘`.
  - O professor escolhe o cenário depois do slide 23 (decisão do responsável); o slide 19 só apresenta o menu. No F4,
    esperar no terminal: as tentativas 1/5…5/5 aparecem ao vivo.
  - `make test_caseN` roda `fN.sh` e depois `audit-fN.sh` (F2, F3, F4, F6, F8) e pelo menos dobra o tempo (F4 passa de
    ~5 min). Usar ao vivo só se o professor pedir a parte do audit-vault (decisão do responsável).
  - `f1.sh`–`f3.sh` e `f5.sh`–`f8.sh` restauram a configuração padrão ao terminar (`restore` em `scripts/demo/lib.sh`);
    `f4.sh` não altera configuração.
  - No macOS, usar `caffeinate -i` para cenários longos (`README.md`).
  - A demo roda na máquina de desenvolvimento (Docker, make, jq, Go e `bin/webrtc-probe` já prontos), com o ambiente
    no ar antes de começar. Instalação em outra máquina: `README.md` ("Instalação passo a passo").
- **Fonte**: `§11`; `README.md` (Cenários de falha); `Makefile` (`test_case%`, `test_audit`); `scripts/demo/lib.sh`,
  `f1.sh` … `f8.sh`, `audit-f*.sh`, `audit-lib.sh:15`; `scripts/aws.sh`.

### Slide 20 — Falha surpresa: previsão (40 s)

- **Objetivo**: `txt:159` — prever antes de executar.
- **Conteúdo** (tabela: o que cai · o que para · o que continua · como volta), linhas curtas:
  - call-signaling (×1) · API (nginx 502), SSE · áudio das chamadas ativas, outbox salvo · `SweepTimeouts`
    compensa PENDING; reentrega após 30 s
  - 1 pod media-relay · áudio daquele pod · outros pods · líder move chamadas em ~3 s; F2
  - todos os media-relay · novas chamadas → `FAILED relay timeout` · chamadas ACTIVE seguem ACTIVE · `reloadOwn`
  - 1 réplica audit-vault · API daquela porta · outra réplica consome · reentrega em 30 s, sem duplicata
  - todo o audit-vault · trilha e gravação · todo o resto (fan-out) · consome o acumulado
  - MiniStack pausado · SQS/SNS/DynamoDB/S3/Secrets · áudio já estabelecido · laços voltam sozinhos (F6)
  - Script pronto: `RELAY_HOST=127.0.0.1 scripts/demo/surprise-kill-call-signaling.sh`
- **Ilustração**: diagrama base com o componente derrubado marcado com ✗ (padrão: `call-signaling`) e as partes
  que continuam funcionando em destaque de sucesso: áudio pontilhado `Navegador ⇢ media-relay`, filas com
  mensagens acumuladas, `signaling-outbox` com itens guardados. Variante por componente se houver tempo de construção:
  um estado do mesmo SVG por linha da tabela (alternado com teclas ou cliques).
- **Notas de demo**: o script `surprise-kill-call-signaling.sh` aceita dois desfechos corretos (caminho A: varredor
  vence, `FAILED relay timeout` e `late relay released`; caminho B: relay responde antes, `call active` ×1). Dizer
  isso **antes** de rodar. Lembrar: um contêiner morto por `docker kill` não volta com `restart: unless-stopped`;
  volta com `docker compose up -d <serviço>` (o script faz isso no `trap`). Pod do media-relay com chamada real no
  navegador: `make kill_relay` (mostra `chamada movida: X -> Y` e os logs `relay pod dead` / `call moved` /
  `picked up`; `DEMO-LAN.md` §3). Para os outros componentes não há script; usar `docker kill <contêiner>` e a
  previsão da tabela. No modo voz, o segundo relay é o serviço `media-relay-2`;
  `RELAY_HOST=127.0.0.1 make start` sobe de volta qualquer contêiner que falte. MiniStack **reiniciado** (não pausado)
  perde todo o estado: evitar.
- **Previsões fora da tabela** (dizer se o professor escolher; `§11.1`):
  - `web` (nginx): param a interface e o proxy `/api/`; seguem o áudio WebRTC já conectado e o `voice-client` (porta
    8081, sem nginx); o navegador recarrega e restaura a sessão pelo cookie de refresh.
  - MiniStack reiniciado: perde tabelas, filas, gravações e as duas chaves; o `infra-init` não roda de novo sozinho;
    recriar com `make stop && RELAY_HOST=127.0.0.1 make start`.
- **Fonte**: `§11.1`; `scripts/demo/surprise-kill-call-signaling.sh`.

### Slide 21 — Decisões de arquitetura (30 s)

- **Objetivo**: resumir as decisões principais (o quê / por quê / alternativa).
- **Conteúdo** (6 cartões, uma linha cada):
  - D1 fan-out SNS → fila por consumidor · vs publicar em cada fila
  - D4 marcador + efeito na mesma transação · vs SQS FIFO (janela de 5 min)
  - D5 outbox no DynamoDB · vs publicar após o commit
  - D8 saga orquestrada · vs coreografia
  - D11 pod dono por chamada, SDP por HTTP · vs SDP por fila / balanceador
  - D12 lease + term + fencing · vs Raft/Paxos
- **Ilustração**: os 6 cartões em grade 3 × 2, cada um com um micro-ícone do recorte correspondente (hexágono com
  duas setas; pilha de 3 itens de transação; tabela outbox com seta; máquina de estados; cliente → pod dono; coroa
  de líder com número de `term`).
- **Fonte**: `§13` (D1, D4, D5, D8, D11, D12).
- **Nota de fala**: recapitulação; D1, D4/D5, D8, D11 e D12 já foram explicadas nos slides 7, 12, 11, 4 e 18. Uma
  frase por cartão, só a alternativa descartada.

### Slide 22 — Limitações conhecidas (30 s)

- **Objetivo**: honestidade sobre o que não está resolvido.
- **Conteúdo**:
  - call-signaling com 1 réplica (hub SSE em memória)
  - MiniStack em memória: `down` apaga tudo
  - Outbox sem ordem; `Joined`/`Left` invertidos no media-relay
  - Sem TTL nos marcadores
  - Gravação melhor esforço; só áudio do navegador
  - Qualquer usuário autenticado entra em chamada ACTIVE com vaga
- **Ilustração**: diagrama base atenuado com 4 marcadores de alerta: sobre `call-signaling ×1` ("SSE em memória"),
  sobre a moldura `MiniStack` ("estado em memória"), sobre `relay-outbox`/`signaling-outbox` ("sem ordem"), sobre
  `audio-chunks` ("melhor esforço").
- **Fonte**: `§14`, D15, D20; `COMO-FUNCIONA.md` §16.6.
- **Nota de fala**: só se perguntado: chave AES sem rotação; `ForgetUser` fora da transação (o join seguinte limpa);
  credenciais `test/test` do MiniStack; audit-vault com no máximo 2 réplicas (portas 8083–8084); num failover, até 5 s
  da gravação se perdem. Bônus (`txt:96-101`) não implementados: `init.sh` é AWS CLI (não Terraform/CDK); API de
  leitura separada, mas não CQRS; rastreio por `correlationId` nos logs, sem OTel; eventos só v1.

### Slide 23 — Encerramento (15 s)

- **Objetivo**: fechar e abrir perguntas.
- **Conteúdo**:
  - 3 serviços · 2 linguagens · 4 categorias de nuvem
  - R1–R6 no código · F1–F8 com um comando cada
  - Repositório: github.com/NicolasPaterno/LarpSpeak
  - Perguntas?
- **Ilustração**: diagrama base completo (slide 4), sem atenuação, menor.
- **Fonte**: `§1`, `§10`, `§11`; `txt:163` (Git público em 03/12/2026).

---

### Apêndice A — um slide por cenário (aberto só para o cenário escolhido)

Formato comum de cada slide AN: título `FN — <nome>`; três blocos curtos **Como provocar** · **Esperado** ·
**O que mostrar**; um recorte do diagrama; o comando em destaque. Linhas de log em inglês, como o código imprime.

#### A1 — F1 Compensação

- **Provocar**: `RELAY_HOST=127.0.0.1 scripts/demo/f1.sh` (sobe 2 relays com `RELAY_FAILURE_RATE=1.0`).
- **Esperado**: chamada `FAILED` em até 15 s; criador fora de `signaling-users`; SSE `call_failed`.
- **Mostrar**: passos da saga ao vivo com hora; Evidência `signaling-calls: ... taken=0 status=FAILED`,
  `signaling-users: (nenhum item)`; `✔ ana recebeu call_failed`; log do relay com `simulated`; trilha do audit-vault `FAILED` com
  `relay failed`, um único evento `CallFailed`, `verified: true`.
- **Recorte**: call-signaling → `relay-requests` → media-relay (✗, dado com "100%") → `relay-results`
  (`RelayFailed`) → call-signaling (`Fail`: `taken=0`, `ForgetUser`) → SSE `call_failed` ao navegador; →
  `SNS call-events` (`CallFailed`) → audit-vault (trilha FAILED).
- **Fonte**: `scripts/demo/f1.sh`; `§11`.

#### A2 — F2 Serviço morre no meio

- **Provocar**: `RELAY_HOST=127.0.0.1 scripts/demo/f2.sh` (`RELAY_LATENCY_MS=8000`, `RELAY_TIMEOUT_MS=90000`; mata a
  réplica que logou `allocating relay`). Só com `make test_case2`: o `audit-f2.sh` mata a réplica do audit-vault que está gravando.
- **Esperado**: `ACTIVE` em até 90 s; exatamente 1 `relay allocated`. Vault: trilhas com 5 eventos únicos, objetos
  S3 únicos.
- **Mostrar** (o script imprime tudo): `allocating relay messageId=<id>`; `docker kill` com hora e `exit=137`;
  `subindo consumer de novo` até `running healthy`; a mesma `messageId` reentregue; Evidência `relay-sessions: 1
  item(s)` (owner, ALLOCATED), `'allocating relay' 2x`, `'relay allocated' 1x`; `✔ F2: chamada ACTIVE, 1 alocação`.
  A reentrega pode cair na réplica reiniciada ou na outra (as duas valem pelo enunciado).
- **Recorte**: fila `relay-requests` → `media-relay #1` (✗, "docker kill") e `media-relay #2`; mensagem
  "invisível 30 s" (selo de texto) → reentregue (a `#1` reiniciada ou a `#2`) → `relay-processed` (marcador) ✓.
- **Fonte**: `scripts/demo/f2.sh`, `scripts/demo/audit-f2.sh`; `§11`, D6.

#### A3 — F3 Mensagem duplicada

- **Provocar**: `RELAY_HOST=127.0.0.1 scripts/demo/f3.sh` (relays parados; o mesmo `RelayAllocated`, mesmo
  `messageId`, 5× em `relay-results`).
- **Esperado**: 1 efeito.
- **Mostrar**: exatamente 1 `call active` e 4 `duplicate ignored` nos logs do call-signaling. Vault: 1 entrada na
  trilha, 1 objeto S3, 8 `duplicate ignored`.
- **Recorte**: 5 envelopes idênticos (mesmo `messageId` em destaque) → `relay-results` → call-signaling →
  transação: 1ª ✓, 2ª–5ª ✗ no item 0 `signaling-processed`.
- **Fonte**: `scripts/demo/f3.sh`, `audit-f3.sh`; `§5.1`.

#### A4 — F4 Mensagem venenosa

- **Provocar**: `RELAY_HOST=127.0.0.1 scripts/demo/f4.sh` (texto que não é envelope em `relay-results`; com
  `make test_case4`, também em `audit-vault-call-events` e `audio-chunks`, ~2,5 min a mais).
- **Esperado**: 5 recebimentos e DLQ (~2,5 min); outra chamada chega a `ACTIVE` normalmente.
- **Mostrar**: `tentativa 1/5` … `5/5`, cada uma com a linha real `poison message left for DLQ`; `relay-results-dlq:
  1 mensagem(ns)`; Evidência com MessageId e body da DLQ (o mesmo publicado); `✔ F4: 5 tentativas`.
- **Recorte**: `relay-results` com a mensagem ruim fazendo um laço "×5 (visibility 30 s)" → `relay-results-dlq`;
  ao lado, uma mensagem válida passando direto para o call-signaling ✓.
- **Fonte**: `scripts/demo/f4.sh`, `audit-f4.sh`; `§2.1`, `§5.3`, D6.

#### A5 — F5 Corrida pelo recurso

- **Provocar**: `RELAY_HOST=127.0.0.1 scripts/demo/f5.sh` (`MAX_PARTICIPANTS=11`; criador + 10 vagas; 100 `join`
  simultâneos).
- **Esperado**: exatamente 10 sucessos, 90 recusas.
- **Mostrar**: placar ao vivo `N/100 respostas: 200=x 409=y` até `200=10 409=90`; Evidência `taken=11 capacity=11`
  e os 11 participantes.
- **Recorte**: igual ao slide 13, com os números do cenário (100 setas → 10 ✓ / 90 ✗).
- **Fonte**: `scripts/demo/f5.sh`; `§6.5`.

#### A6 — F6 Broker fora do ar

- **Provocar**: `RELAY_HOST=127.0.0.1 scripts/demo/f6.sh` (`docker compose pause ministack` por 30 s com uma chamada
  em andamento).
- **Esperado**: reconexão sozinha, outbox drena, nenhuma mensagem perdida.
- **Mostrar**: `POST /calls` durante a queda → 503; `broker pausado há Ns | avisos de retry: <pod>=n` a cada 5 s; a
  chamada anterior fica `ACTIVE`; outbox caindo até 0 (`signaling-outbox`, `relay-outbox`); pods sem reinício
  (`RestartCount` 0); nova chamada `ACTIVE`. `pause`, não `stop`: o `stop` apagaria o estado em memória. Vault: cada réplica logou
  `broker unreachable, retrying` e nenhuma reiniciou.
- **Recorte**: igual ao slide 17 (linha do tempo da pausa), com os três serviços e as duas tabelas de outbox.
- **Fonte**: `scripts/demo/f6.sh`, `audit-f6.sh`; `§5.3`, D7.

#### A7 — F7 Resposta tardia

- **Provocar**: `RELAY_HOST=127.0.0.1 scripts/demo/f7.sh` (`RELAY_LATENCY_MS=6000` > `RELAY_TIMEOUT_MS=3000`).
- **Esperado**: compensação correta do resultado atrasado.
- **Mostrar**: chamada `FAILED`; `relay-sessions` com `RELEASED` e motivo `late allocation`; trilha `FAILED`
  `relay timeout`, sem `CallStarted`.
- **Recorte**: linha do tempo: t=0 `RelayRequested`; t≈3 s `SweepTimeouts` → `FAILED` (✗); t≈6 s
  `RelayAllocated` chega → `Ack` + `ReleaseRelayRequested` → media-relay `RELEASED (late allocation)`.
- **Fonte**: `scripts/demo/f7.sh`; `§6.4`, `§8.2`.

#### A8 — F8 Escala horizontal

- **Provocar**: `RELAY_HOST=127.0.0.1 scripts/demo/f8.sh` (3 réplicas do media-relay, 30 chamadas). Vault: 2
  réplicas, 4 chamadas.
- **Esperado**: carga dividida, nenhuma chamada alocada duas vezes.
- **Mostrar**: placar ao vivo por réplica até `total 30/30`; Evidência `log: larpspeak-media-relay-N: X chamadas` e
  `banco: relay-sessions owner=<hostname> (larpspeak-media-relay-N): X sessões`, só desta rodada; `✔ F8: carga
  dividida` (falha se alguma réplica ficar com 0).
- **Recorte**: fila `relay-requests` → 3 réplicas com barras proporcionais às contagens (preenchidas na hora pelo
  apresentador ou deixadas genéricas).
- **Fonte**: `scripts/demo/f8.sh`, `audit-f8.sh`; `§8.5`.

---

## 4. Plano B para a demo ao vivo

Se a demo falhar (ambiente não sobe, script trava, rede/Docker lento):

Sem saída gravada (decisão do responsável): os scripts mostram a evidência ao vivo.

1. Ordem de recuperação ao vivo:
   - script falhou no meio: ele restaura a configuração no `trap`; rodar de novo uma vez;
   - ambiente quebrado: `make stop && make start` (leva tempo; MiniStack zera);
   - sem tempo: explicar com o apêndice AN e o recorte do diagrama.
2. Alternativa leve que não depende de script: abrir os testes unitários que provam o mesmo efeito
   (`CS/internal/adapter/dynamo/store_test.go`, `services/audit-vault.Tests/`). `make test` leva minutos: opcional,
   fora do tempo.
3. Falha surpresa: se o professor pedir algo sem script, mostrar a linha da tabela do slide 20 (previsão), executar
   `docker kill <contêiner>`, observar `make logs`, e voltar com `docker compose up -d <serviço>`.

---

## 5. Instruções de construção

Para quem for construir o deck:

- **Arquivo**: `presentation/deck/index.html`, autocontido (HTML + CSS + JS inline). Nenhuma requisição de rede:
  sem CDN, sem fontes remotas, sem imagens externas. Fontes e cores exatamente como em `presentation/DESIGN.md`
  (se DESIGN.md pedir uma fonte, embutir como `@font-face` com arquivo local em `presentation/deck/` ou usar a pilha
  de fontes do sistema que DESIGN.md indicar).
- **Estrutura**: um `<section class="slide" id="s1">` … `id="s23"` por slide (5b e 5c são `id="s5b"` e `id="s5c"`, entre `s5` e `s6`), depois `id="a1"` … `id="a8"`.
  Cada `section` contém: título, lista curta de tópicos, um `<svg>` inline com a ilustração especificada, e um
  `<aside class="notes">` (oculto) com as notas de fala e a fonte.
- **Diagramas**: SVG inline desenhado à mão, um `<symbol>` por componente recorrente (serviço, cilindro, fila,
  hexágono, balde, cadeado, cliente) reutilizado com `<use>`. Os rótulos são `<text>` reais (não caminhos), com
  identificadores em fonte monoespaçada. Recortes = mesmo desenho base com classes `dim` (atenuado) e `hl`
  (destaque). Todo texto dos diagramas deve bater com ARCHITECTURE.md (nomes de fila, tabela, tipo de mensagem).
- **Navegação por teclado**: `→`/`Espaço`/`PageDown` avança; `←`/`PageUp` volta; `Home`/`End`; `1`–`9` + `Enter`
  vai ao slide N; `a` + dígito vai ao apêndice; `n` mostra/oculta as notas; `t` mostra o cronômetro e o tempo-alvo
  do slide atual; `f` entra em tela cheia. O hash da URL (`#s7`, `#a3`) reflete o slide atual e abre direto nele.
- **Um slide por tela**: só a `section` ativa visível; layout fixo 16:9 escalado para a janela (sem rolagem).
- **Densidade e legibilidade** (`DESIGN.md` 3.3, 4.4–4.6, 7): as listas "Conteúdo" deste plano são o roteiro, não o
  texto da tela. Na tela: no máximo 3 itens no layout diagrama + bullets; tabelas com no máximo 6 linhas × 4 colunas;
  painéis de código/log com no máximo 12 linhas. O excedente vai para o `<aside class="notes">`. Texto legível com
  piso de 24px (22px permitido dentro de diagramas densos, decisão do responsável), no máximo 5
  tamanhos de texto por slide e no máximo um elemento animado em loop por slide.
- **Links internos**: os cartões do slide 19 levam aos slides A1–A8; cada A tem link de volta ao slide 19.
- **Tempo**: guardar o tempo-alvo de cada slide em `data-seconds` na `section`; o cronômetro (`t`) mostra
  decorrido × acumulado-alvo.
- **Sem conteúdo inventado**: onde este plano tem `[PREENCHER: ...]`, manter o marcador visível até a resposta em
  `presentation/QUESTIONS.md`.
- **Validação**:
  1. Abrir `presentation/deck/index.html` direto no navegador (`file://`), com a rede desligada: nada pode faltar.
  2. Percorrer todos os slides só com o teclado; conferir que cada um tem ilustração.
  3. Somar `data-seconds` dos slides 1–23, 5b e 5c: deve dar 955 s (~16 min), abaixo de 20 min.
  4. Ensaiar uma vez com o cronômetro (`t`); ajustar cortes de fala, não o conteúdo exigido.
  5. Conferir a tabela de cobertura (seção 2) contra o deck: cada linha deve apontar para um slide existente.
  6. Conferir os nomes nos diagramas contra `ARCHITECTURE.md` §1.1, §2.1, §3 e contra `docker-compose.yml`.
  7. Testar em 1920×1080 (projetor 16:9).
