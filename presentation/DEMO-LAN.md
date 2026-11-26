# Demo na LAN: chamada real entre 2 computadores

Duas provas, com uma chamada de voz real pelo navegador:

1. **Failover**: derrubar o pod do media-relay que hospeda a chamada. O áudio volta sozinho pelo outro pod.
2. **Gravação segura**: o áudio está cifrado no S3 e dá para ouvir a trilha de cada participante.

- **PC A**: roda a infra e é um dos participantes.
- **PC B**: outro participante, na mesma rede.

---

## 1. Preparação (antes da apresentação)

No PC A, na raiz do repositório:

```bash
make build                                  # imagens + clientes
make certs                                  # só na 1ª vez (precisa do mkcert); HTTPS para o microfone
make start                                  # RELAY_HOST = IP do en0 (Wi-Fi)
docker compose up -d --force-recreate web   # só depois de um make certs: o nginx carrega os certificados ao iniciar
```

Confira o ambiente:

```bash
ipconfig getifaddr en0                                   # IP do PC A, ex.: 192.168.18.214
docker exec larpspeak-media-relay-1 printenv RELAY_HOST  # tem que ser o mesmo IP, não 127.0.0.1
```

> Se a rede mudar (outro Wi-Fi, outro IP), rode `make start` de novo. Os relays anunciam o IP antigo até serem recriados.

## 2. Entrar na chamada

| | PC A | PC B |
|---|---|---|
| Endereço | `https://localhost:8443` | `https://<IP do PC A>:8443` |
| Certificado | confiável (mkcert) | aviso: "Avançado" e depois "Continuar" |
| Usuário | ex.: `nicolas` | ex.: `baldo` |
| Ação | criar a chamada e convidar o B | aceitar o convite e permitir o microfone |

Fale nos dois lados e confirme que o áudio está indo e voltando.

## 3. Prova 1: failover do relay

Com a chamada em andamento, no PC A:

```bash
make kill_relay                      # acha a chamada ativa sozinho
make kill_relay CALL=<callId>        # se houver mais de uma chamada ativa
```

O que o script faz e mostra:

1. Busca na tabela `relay-sessions` o pod dono da chamada (`owner`) e dá `docker kill` nele.
2. Espera o dono mudar e imprime `chamada movida: <pod morto> -> <pod novo>`.
3. Mostra os logs do failover:
   - `relay pod dead: no ping, no health answer`: o líder viu o pod parado há mais de 3 s, e o `/health` dele não respondeu.
   - `call moved from dead relay pod`: o líder moveu a chamada para o pod com menos sessões.
   - `call picked up after failover`: o pod novo assumiu a chamada.

Durante o teste, escute: o áudio some por cerca de 3–5 s e volta. O navegador recebe `relay_moved` pelo SSE e refaz a
oferta WebRTC no pod novo. Ninguém clica em nada.

Para religar o pod morto, rode o comando que o script imprime no fim:

```bash
docker start <nome do contêiner>
```

> Se você chamar `make kill_relay` de novo depois, ele derruba o pod que agora hospeda a chamada. Religue o primeiro
> antes, senão não sobra pod para o failover.

## 4. Prova 2: gravação segura e áudio de cada participante

Funciona com a chamada em andamento ou já encerrada. Os trechos vão para o S3 a cada 5 s (`AUDIO_CHUNK_MS`).

```bash
make recording                       # chamada mais recente
make recording CALL=<callId>
```

| Passo | O que prova |
|---|---|
| 1. Trechos no S3 | quantos trechos de 5 s existem por participante em `s3://larpspeak-audio/audio/<callId>/<userId>/` |
| 2. Objeto cifrado | o `xxd` mostra o cabeçalho `LSA1`, seguido de bytes aleatórios (nonce, tag, ciphertext). O `file` diz `data`: não é áudio tocável |
| 3. Chave fora do bucket | a chave AES-256 está no Secrets Manager (`larpspeak/audio-key`); o bucket só tem `audio/` |
| 4. API com token | exportar sem `Authorization: Bearer` responde **401** |
| 5. Integridade | um trecho de um participante copiado para a pasta do outro dá **500** `recording failed authentication`. O AES-GCM autentica callId, userId e o início do trecho. O script remove a cópia, e a exportação volta a dar 200 |
| 6. Áudio | baixa um `.ogg` (Opus) decifrado por participante em `/tmp/larpspeak-recordings/<callId>/` e abre no Chrome |

Para ouvir de novo sem rodar tudo:

```bash
open -a "Google Chrome" /tmp/larpspeak-recordings/<callId>/*.ogg
```

> Se houve failover, a trilha tem um buraco de até 5 s. O trecho aberto na memória do pod morto se perde junto com o
> processo.

Para fazer manualmente, sem o script:

```bash
T='Authorization: Bearer larpspeak-audit-demo'
curl -s -H "$T" 'localhost:8083/audit/calls?limit=1' | jq -r '.[0].callId'     # callId mais recente
scripts/aws.sh s3 ls s3://larpspeak-audio/audio/<callId>/ --recursive          # trechos cifrados
curl -s -H "$T" localhost:8083/audit/calls/<callId>/audio | jq                 # trilhas por participante
curl -H "$T" localhost:8083/audit/calls/<callId>/audio/<userId> -o <userId>.ogg
```

## 5. Se algo der errado

| Sintoma | Causa provável | O que fazer |
|---|---|---|
| O PC B não abre a página | firewall do macOS no PC A, ou redes diferentes | Ajustes > Rede > Firewall: permitir Docker; usar o mesmo Wi-Fi |
| A página abre, mas não há áudio | `RELAY_HOST` errado, ou UDP 50000/50001 bloqueado | `printenv RELAY_HOST` (seção 1); `make start` |
| O microfone é negado no PC B | o certificado não foi aceito | aceite o aviso, ou instale `$(mkcert -CAROOT)/rootCA.pem` no PC B |
| `make kill_relay`: "informe o callId" | 0 ou mais de 1 chamada ativa | `make kill_relay CALL=<callId>` (o callId aparece na URL da chamada) |
| O áudio não volta depois do kill | o outro relay também estava fora | `docker ps \| grep media-relay`; `make start` |
| `make recording`: "nenhum trecho gravado" | a chamada durou menos de 5 s, ou o audit-vault está fora | fale por mais tempo; `docker ps \| grep audit-vault` |

> Nunca reinicie o MiniStack (`restart`) na apresentação. Ele guarda tudo em memória: tabelas, filas, gravações e
> chaves se perdem. Para zerar de propósito: `make stop && make start`.
