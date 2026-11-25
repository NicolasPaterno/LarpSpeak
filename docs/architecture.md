# Arquitetura: auditoria e gravação de chamadas (audit-vault)

Este documento descreve o **audit-vault** e a parte do **media-relay** que grava o áudio das chamadas:
o que cada peça faz, por que foi feita assim e o que acontece quando algo falha.

## Visão geral

```
                    SNS call-events (fan-out)
call-signaling ──┐        │
media-relay ─────┴──► ────┼──► SQS media-relay-call-events ──► media-relay
                          └──► SQS audit-vault-call-events ──► audit-vault ──► DynamoDB audit-records
                                                                          └──► DynamoDB audit-processed
navegador ──WebRTC/Opus──► media-relay (SFU)
                               │ cópia de cada pacote (Recorder)
                               ▼
                          SQS audio-chunks ──► audit-vault ──AES-256-GCM──► S3 larpspeak-audio
                                                    ▲
                                       Secrets Manager larpspeak/audio-key

Leitura: GET /audit/... (token Bearer) ──► audit-vault ──► DynamoDB / S3 ──► JSON ou .opus
```

O audit-vault tem duas responsabilidades:

1. **Trilha de auditoria**: guarda, sem alterar, todo evento de chamada (`CallStarted`, `ParticipantJoined`,
   `CallEnded`, `CallFailed`...) e reconstrói o estado de cada chamada a partir deles.
2. **Gravações**: recebe o áudio dos participantes em trechos, cifra cada um e guarda no S3. Também devolve
   um arquivo `.opus` por participante.

Localmente, a AWS é emulada pelo **MiniStack** (SQS, SNS, S3, DynamoDB e Secrets Manager na porta 4566).
O código é o mesmo da AWS real: só muda `AWS_ENDPOINT_URL`.

## Requisitos e cenários de falha da especificação

| Código | Significado | Como o audit-vault atende |
|---|---|---|
| R1 | Idempotência: efeito aplicado uma vez só | Marcador `messageId` em `audit-processed`; na gravação, chave S3 determinística |
| R2 | Controle de concorrência | Escrita condicional (`attribute_not_exists`) no DynamoDB |
| R4 | Escala horizontal | Consumidores concorrentes: várias réplicas leem a mesma fila |
| R6 | Sobreviver à queda do broker | Laço de consumo com backoff; recurso lido sob demanda, nunca só no início |
| F2 | Serviço morre no meio | Mensagem só é apagada depois de gravada, então é reentregue |
| F3 | Mensagem duplicada | Mesmo `messageId` produz um único efeito |
| F4 | Mensagem venenosa | Não é apagada; após 5 recebimentos vai para a DLQ |
| F6 | Broker fora do ar | Consumidor tenta de novo com backoff até 5 s e volta sozinho |
| F8 | Várias réplicas | Fila SQS divide as mensagens; idempotência evita efeito duplicado |

## Decisões de arquitetura

### D1. Comunicação só por mensagens (SNS → SQS), nunca por chamada direta

Os produtores publicam fatos no tópico `call-events`. Cada consumidor tem **sua própria fila** assinada
no tópico (fan-out), com `RawMessageDelivery=true` para receber o envelope sem o invólucro do SNS.

- Se o audit-vault estiver fora do ar, os eventos esperam na fila dele; nenhum outro serviço é afetado.
- Adicionar um consumidor novo não exige mudar quem publica.

### D2. Envelope único e contratos versionados

Toda mensagem segue `contracts/envelope.schema.json`: `messageId`, `correlationId` (o `callId`), `type`,
`version`, `occurredAt`, `payload`. Cada tipo de evento tem seu schema em `contracts/events/*.v1.schema.json`.
O audit-vault valida o envelope em `Envelope.TryParse`. O que não passa é tratado como **venenoso**.

### D3. Entrega pelo menos uma vez + consumidor idempotente

O SQS pode entregar a mesma mensagem mais de uma vez. Em vez de tentar "exatamente uma vez" na entrega,
o efeito é que é idempotente:

- **Eventos de chamada** (`DynamoAuditStore.RecordAsync`): uma única **transação** DynamoDB grava o marcador
  em `audit-processed` (com `attribute_not_exists(pk)`) e acrescenta o evento à trilha em `audit-records`.
  Ou as duas coisas acontecem, ou nenhuma. Se o marcador já existe, a transação falha no item 0 e a
  mensagem é reconhecida como duplicada.
- **Áudio** (`AudioProcessor`): o S3 não participa de transação com o DynamoDB. A chave do objeto é
  **determinística** (`audio/{callId}/{userId}/{startMs:D13}-{messageId}.bin`), então reprocessar a mesma
  mensagem sobrescreve o mesmo objeto. A sequência é: consulta o marcador → grava no S3 → grava o marcador.
  Se o processo morrer entre o upload e o marcador, a reentrega só regrava o mesmo objeto.

### D4. A mensagem só é apagada depois do efeito

`QueueConsumer` só chama `DeleteMessage` quando o resultado é `Recorded` ou `Duplicate`.

| Resultado | Apaga da fila? | O que acontece depois |
|---|---|---|
| `Recorded` | sim | fim |
| `Duplicate` | sim | fim (nenhum efeito novo) |
| `Failed` (DynamoDB/S3/segredo fora) | não | volta após o visibility timeout (30 s) |
| `Poison` (contrato inválido) | não | após 5 recebimentos o SQS move para `*-dlq` |

Cada fila tem DLQ com `maxReceiveCount = 5`, criada em `infra/init/init.sh`.

### D5. Laço de consumo resiliente e parada graciosa

- Erros do broker nunca encerram o laço: backoff exponencial de 250 ms até 5 s (R6/F6).
- A URL da fila e a chave AES são obtidas sob demanda e só guardadas depois de obtidas com sucesso.
  O serviço sobe mesmo com o MiniStack ainda fora do ar.
- Ao parar, o long poll em andamento (10 s) termina e o lote recebido é processado antes de sair.
  Por isso `ShutdownTimeout = 15 s` e `stop_grace_period: 20s` no compose.

### D6. Trilha append-only e estado derivado

Cada chamada é um item `call#{callId}` com a lista `events`, onde só se acrescenta (`list_append`).
Cada evento guarda o payload original e o **SHA-256 da mensagem recebida**, para provar depois que ele
não foi alterado. O estado (`PENDING`, `ACTIVE`, `ENDED`, `FAILED`, participantes, duração) **não é
gravado**: é recalculado a cada leitura em `CallTrail.Build`, ordenando por `occurredAt`. Assim o
resultado não depende da ordem em que as mensagens chegaram.

### D7. Gravação fora do caminho do áudio (media-relay)

O `Recorder` (Go, `internal/app/recorder.go`) recebe uma cópia de cada pacote Opus que o navegador envia:

- `Record` só acrescenta o pacote na memória. Ele **nunca bloqueia** a chamada.
- O trecho é fechado a cada `AUDIO_CHUNK_MS` (5 s), ao atingir 96 KiB (abaixo do limite de 256 KiB do SQS
  depois do base64), quando o participante sai ou quando a conexão dele é trocada (nova linha do tempo RTP).
  Um verificador por segundo fecha os trechos de quem parou de mandar pacotes.
- Os trechos prontos vão para um canal com capacidade 512. Se encher (broker lento), o trecho é
  **descartado e contado** (`Dropped`). A gravação perde dados antes de atrasar a voz.
- A publicação tenta até 6 vezes com backoff. No desligamento, os trechos pendentes são enviados com
  5 s de prazo.
- Desligado se `AUDIO_CHUNKS_QUEUE` não estiver definida.

Os pacotes Opus são gravados **como chegaram, sem decodificar**. É barato e não perde qualidade.

### D8. Criptografia em camada de aplicação (AES-256-GCM) + SSE no bucket

`AudioCrypto` cifra cada trecho antes de enviar ao S3:

```
"LSA1" (4 bytes) | nonce (12, aleatório por trecho) | tag (16) | texto cifrado
dados autenticados (AAD) = "{callId}\n{userId}\n{startMs}"  (não gravados; refeitos a partir da chave S3)
```

- **GCM** cifra e autentica: um bit alterado faz a abertura falhar.
- A **AAD** amarra o trecho ao seu lugar. Um trecho copiado para outra chamada, outro usuário ou outro
  horário não abre (`recording failed authentication`, HTTP 500). Nunca é servido áudio que falhou na autenticação.
- A chave de 32 bytes fica no **Secrets Manager** (`larpspeak/audio-key`). O serviço guarda só o nome do segredo.
- O bucket também tem criptografia padrão (SSE AES256) e acesso público bloqueado. É defesa em profundidade:
  o S3 sozinho não protege contra quem tem acesso de leitura ao bucket, por isso a criptografia própria.

### D9. Exportação para Ogg Opus sem decodificar

`AudioExporter` lista os trechos do participante (em ordem pela chave), abre cada um, junta os pacotes e
`OggOpus.Write` monta um arquivo Ogg Opus (RFC 7845) com cabeçalhos `OpusHead`/`OpusTags`, páginas com
CRC-32 e granule position calculada a partir do byte TOC de cada pacote (RFC 6716).

### D10. API de leitura protegida

`GET /health` é aberto. Tudo em `/audit/*` exige `Authorization: Bearer <AUDIT_API_TOKEN>`. A comparação
é feita em tempo constante sobre o SHA-256 dos dois valores. Os IDs que viram parte da chave S3 aceitam
só `[A-Za-z0-9_-]{1,64}` (sem `/` nem `..`).

### D11. Um processo, dois consumidores

O audit-vault roda um `QueueConsumer` por fila (`audit-vault-call-events` e, com gravação ligada,
`audio-chunks`), cada um com seu `IMessageHandler` (`MessageProcessor` e `AudioProcessor`). Eles são
registrados por `ConsumerRegistration.AddQueueConsumer<THandler>`, que usa
`AddSingleton<IHostedService>` **de propósito**. Veja "Correção: consumidor de áudio que nunca rodava".

## Classes principais

| Classe | Arquivo | Papel |
|---|---|---|
| `Envelope` | `Envelope.cs` | Valida o envelope comum |
| `QueueConsumer` / `SqsQueue` | `QueueConsumer.cs` | Long poll, backoff, apagar só após o efeito |
| `ConsumerRegistration` | `QueueConsumer.cs` | Registra um consumidor por fila |
| `MessageProcessor` | `MessageProcessor.cs` | Evento de chamada → trilha |
| `DynamoAuditStore` | `DynamoAuditStore.cs` | Transação marcador + trilha |
| `CallTrail` / `AuditEvent` | `AuditModels.cs` | Estado derivado e evento imutável com SHA-256 |
| `AudioChunk` / `ChunkCodec` / `AudioKeys` | `AudioChunks.cs` | Valida o trecho, formato binário, chaves S3 |
| `AudioCrypto` | `AudioCrypto.cs` | AES-256-GCM com AAD |
| `AudioProcessor` / `AudioExporter` | `AudioProcessor.cs` | Grava o trecho; monta o `.opus` |
| `S3AudioStore` / `SecretsManagerKeyProvider` / `DynamoProcessedStore` | `AudioStorage.cs` | Adaptadores AWS |
| `OggOpus` | `OggOpus.cs` | Escreve o arquivo Ogg Opus |
| `AuditApi` | `AuditApi.cs` | Rotas HTTP e token |
| `Recorder` (Go) | `media-relay/internal/app/recorder.go` | Corta e publica os trechos |

## Configuração

| Variável | Serviço | Padrão | Efeito |
|---|---|---|---|
| `CALL_EVENTS_QUEUE`, `AUDIT_TABLE`, `PROCESSED_TABLE` | audit-vault | obrigatórias | Trilha de auditoria |
| `AUDIO_CHUNKS_QUEUE` | ambos | vazio = gravação desligada | Liga a gravação |
| `AUDIO_BUCKET`, `AUDIO_KEY_SECRET` | audit-vault | obrigatórias com gravação | Destino e chave |
| `AUDIT_API_TOKEN` | audit-vault | `larpspeak-audit-demo` no compose | Token de `/audit/*` |
| `AUDIO_CHUNK_MS` | media-relay | 5000 (mínimo 1000) | Duração de cada trecho |

## Correção: consumidor de áudio que nunca rodava

**Sintoma:** com a gravação ligada, as mensagens ficavam em `audio-chunks` sem ninguém consumir e o bucket
continuava vazio. Os eventos de chamada eram processados normalmente.

**Causa:** os dois consumidores eram registrados com `AddHostedService(sp => new ConsumerService(...))`.
Por dentro, o `AddHostedService` usa `TryAddEnumerable`, que ignora em silêncio um segundo registro com o
mesmo tipo de implementação. Como os dois eram `ConsumerService`, só o primeiro (eventos de chamada) rodava.
Os testes unitários usavam fakes e não montavam o host, então não pegaram o problema.

**Correção:** `ConsumerRegistration.AddQueueConsumer<THandler>` registra com `AddSingleton<IHostedService>`,
que não descarta duplicados, e escreve no log `consumer started for queue ...` para cada fila.
`ConsumerRegistrationTests` confere que há dois consumidores com a gravação ligada, um com ela desligada,
e documenta o comportamento do `AddHostedService`.

**Verificação de ponta a ponta:** um `AudioChunk` publicado em `audio-chunks` gerou o objeto
`audio/callteste1/willian/...bin` (começa com `LSA1`), `GET /audit/calls/callteste1/audio/willian` devolveu
um `OggS` válido, e o mesmo trecho copiado para outro usuário devolveu `recording failed authentication`.

## Limitações conhecidas

- Só o áudio do navegador (WebRTC) é gravado. O cliente UDP de terminal (`voice-client`) não é.
- Silêncios entre trechos (queda e retorno) não são preenchidos: o `.opus` toca os trechos emendados.
- A gravação é melhor esforço: com o broker fora por muito tempo, trechos podem ser descartados (e contados).
- Sem `AUDIT_API_TOKEN`, `/audit/*` fica aberto, inclusive o áudio decifrado.
- `AUDIO_CHUNK_MS` não tem limite superior. Com pacotes de 20 ms, trechos acima de ~100 s passam do limite
  de 5000 frames do vault e vão para a DLQ.
- A chave AES é única e sem rotação; trocá-la torna as gravações antigas ilegíveis.
- No MiniStack tudo fica em memória: derrubar o ambiente apaga trilhas, gravações e a chave.
