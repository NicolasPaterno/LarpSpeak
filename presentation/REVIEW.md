# Revisão do plano da apresentação

Revisão de `presentation/PLAN.md` contra o enunciado (`.ai/assignment/trabalho-final.txt`, citado como `txt:<linha>`),
`presentation/DESIGN.md`, `ARCHITECTURE.md` e o código (que prevalece). Data: 2026-11-25.

## Veredito

**Aprovado com ressalvas, depois das correções aplicadas.** O plano cobre todo o enunciado e quase todas as
afirmações batem com o código. O único problema bloqueante era a demo: `make test_caseN` roda também os cenários do
audit-vault e não cabia na janela de 150 s (F4 passava de 5 min). Isso foi corrigido. Ficam perguntas para o
responsável, principalmente se rodar um script conta como "provocar manualmente" (`txt:120`).

- Achados: **1 bloqueante, 11 importantes, 10 menores**.
- Aplicados no `PLAN.md`: 1 bloqueante, 10 importantes, 2 menores.
- Pendentes: 1 importante (pergunta ao responsável) e nenhum menor.
- Deck final: **23 slides + 8 apêndices (A1–A8)**, **985 s = 16 min 25 s** (835 s de fala + 150 s de demo). Com a
  falha surpresa executada pelo script (~1–1,5 min), o total fica em ~18 min, abaixo do teto de 20 min.

## O que foi conferido e está correto

- Cobertura: cada item do enunciado tem slide (tema e fluxo, 3 elementos, diagrama, justificativa de nuvem e broker,
  4 categorias, linguagens, banco por serviço, contratos/envelope, Dockerfile + health + compose, R1–R6 com código,
  R4 com ordenação, F1–F8, falha surpresa com previsão, infra por script, variáveis de ambiente).
- Referências de código conferidas: `handlers.go:46` e `:82` (202), `store.go:169` (`Join`), `:181` (`Leave`), `:380`
  (`commit`), `saga.go:159-171` (`compensate`), `:376-397` (`SweepTimeouts`), `relay.go:176-219` (`allocate`),
  `nodes.go:56/94/118` (`Claim`/`MarkDead`/`Move`), `bus.go:114-138` (outbox), `:140-151` (SNS), `init.sh:6-63`,
  `docker-compose.yml:3, 8-13, 46-51, 75, 79-80, 88-91, 99, 105-106`, `domain/envelope.go:21-52`,
  `Envelope.cs:19-52`, 12 schemas em `contracts/events/`, adaptadores Go e interfaces C#.
- Comandos: `make start`, `make logs`, `make test`, `make test_audit`, `make test_case%`, `scripts/aws.sh`,
  `scripts/demo/f1.sh`–`f8.sh`, `audit-f2/3/4/6/8.sh`, `surprise-kill-call-signaling.sh` existem.
- Linhas de log citadas nos apêndices (`allocating relay`, `relay allocated`, `call active`, `duplicate ignored`,
  `poison message left for DLQ`, `late relay released`, `call compensated`, `relay released`) existem no código.
- Placeholders `[PREENCHER]` estão marcados e têm pergunta em `QUESTIONS.md`.

## Achados

| # | Severidade | Slide | Evidência | Correção | Estado |
|---|---|---|---|---|---|
| B1 | bloqueante | 20 (agora 19), A1–A8 | `Makefile` `test_case%` roda `fN.sh` **e** `audit-fN.sh` (F2, F3, F4, F6, F8). `f4.sh` espera ~2,5 min e `audit-f4.sh` mais ~2,5 min; `audit-f2.sh` e `audit-f6.sh` esperam até 90 s por chamada. A janela de 150 s e o teto de 20 min estouram | Ao vivo: `RELAY_HOST=127.0.0.1 scripts/demo/fN.sh`. `make test_caseN` fica para gravar evidências e para quando o professor pedir o audit-vault. Durações estimadas por cenário; roteiro de F4 (iniciar, seguir para os slides 20–22, voltar ao terminal) | aplicado |
| I1 | importante | 19 e 20 | Os dois slides eram menus dos mesmos apêndices A1–A8 (repetição). A tabela do slide 19 tinha 8 linhas; `DESIGN.md` 4.5 limita a 6 | Fundidos no slide 19 "F1–F8 e demo ao vivo", com grade 2 × 4 de cartões. Deck com 23 slides; renumeração das referências em `PLAN.md` e `QUESTIONS.md` | aplicado |
| I2 | importante | 18, orçamento | 30 s (~65 palavras a 130/min) para explicar 8 passos de failover não é realista | Slide 18 com 45 s e 5 passos; slide 5 de 30 para 20 s (as linguagens já aparecem no slide 4); decisões de 40 para 30 s (recapitulação); menu F1–F8 de 45 para 30 s. Total 985 s | aplicado |
| I3 | importante | todos os diagramas | "Componentes visuais recorrentes" contradizia `DESIGN.md` §5.2–5.3: serviço arredondado (DESIGN: raio 0), fila com três divisórias (duas barras), S3 como balde e Secrets Manager como cadeado (cilindros), seta HTTP tracejada e mensagem cheia (DESIGN: HTTP amber contínua, mensagem blue tracejada `10 6`) | Bloco reescrito conforme `DESIGN.md` §5, com remissão explícita | aplicado |
| I4 | importante | 3, 11, 12, 13, 15, 16, 17, 18, 21, A1–A8 | ✓, relógio, dado, ampulheta e coroa não existem em `web/src/components/Icons.tsx` (só há `X` para falha); `DESIGN.md` §8 proíbe ícones de fora e emoji | Regra de marcadores: sucesso = selo signal com palavra; falha = ícone `X` red com palavra; ícones ausentes viram selos de texto (`200 ms`, `100%`, `30 s`, `LÍDER · term 7`) | aplicado |
| I5 | importante | 12, 15, 17, 19, 20 | Slides com 5 a 9 itens; `DESIGN.md` 4.4 pede exatamente 3 itens no layout diagrama + bullets, 4.5 até 6 × 4 em tabela, 4.6 até 12 linhas de código; a seção 5 do plano não citava o piso de 24px | Regra de densidade e legibilidade na seção 5: "Conteúdo" é roteiro; o excedente vai para as notas; piso de 24px; até 5 tamanhos de texto e um loop por slide | aplicado |
| I6 | importante | 17, A6 | O slide mostrava `broker unreachable, retrying` para todos. No Go essa linha só sai antes de a URL da fila ser resolvida (`CS/MR bus.go:79`); durante F6 sai `receive failed, retrying` (`bus.go:87`) e `outbox scan failed, retrying` (`bus.go:119`). Só o C# usa `broker unreachable, retrying` (`QueueConsumer.cs:67`). Backoff real: 250 ms, 500 ms, 1 s, 2 s, 4 s, 5 s (`bus.go` `backoff`) | Linhas por linguagem e sequência corrigida no slide 17 | aplicado (a divergência em `ARCHITECTURE.md` §5.3 virou pergunta) |
| I7 | importante | 9 | `txt:67` exige o formato (AsyncAPI, JSON Schema ou Protobuf); o slide não dizia qual | "Contratos em JSON Schema" no conteúdo | aplicado |
| I8 | importante | A2, A5, A8 | `txt:120` pede mostrar "pelos logs e pelo estado dos bancos"; esses apêndices só tinham logs ou saída da API | Comandos `scripts/aws.sh dynamodb get-item` em `relay-sessions` (A2, A8) e `signaling-calls` (A5); nome real dos contêineres em A8 | aplicado |
| I9 | importante | 21 (agora 20) | `ARCHITECTURE.md` §11.1 tem 8 linhas de previsão; o slide tinha 6, sem `web` nem MiniStack reiniciado. No modo voz o segundo relay é o serviço `media-relay-2` (`compose.voice.yml`), que `docker compose up -d <serviço>` sem os dois arquivos não traz de volta | Previsões de `web` e MiniStack reiniciado nas notas; recuperação por `RELAY_HOST=127.0.0.1 make start` | aplicado |
| I10 | importante | 20 (agora 19) | `$AUDIT_API_TOKEN` não existe no shell do host (só no contêiner, padrão `larpspeak-audit-demo`, `docker-compose.yml:108`): o `curl` daria 401. `make start` não reconstrói imagens (`ARCHITECTURE.md` §1.3). `audit-lib.sh:49` usa python3 | Token escrito por extenso; `make build` antes; python3 e Go nos pré-requisitos | aplicado |
| I11 | importante | 19, A1–A8 | `txt:120`: "o grupo a provoca manualmente, ao vivo". Os scripts automatizam provocação e conferência | Decisão do responsável: aceitar o script narrado ou preparar roteiro manual por cenário | pendente (pergunta em `QUESTIONS.md`) |
| M1 | menor | A8 | `f8.sh` imprime o nome do contêiner (`larpspeak-media-relay-N`), não `media-relay-N` | Corrigido junto com I8 | aplicado |
| M2 | menor | 19 | `scripts/aws.sh` usa a rede `larpspeak_default`: o clone precisa estar num diretório `larpspeak` | Nos pré-requisitos da demo | aplicado |
| M3 | menor | 10 | O diagrama de partida põe `web` depois de `infra-init` com `service_completed_successfully`; o `web` depende só de `call-signaling` (`docker-compose.yml:63`) | Ligar `web` a `call-signaling` no desenho | aplicado |
| M4 | menor | 10 | "Recriar do zero: `docker compose down -v && docker compose up --build`" cita o `README.md`, que não tem esse comando (tem `make stop`/`make build`/`make start`) | Citar `docker-compose.yml:1` e `ARCHITECTURE.md` §1.3, ou acrescentar ao README no fechamento | aplicado (README: "Recriar o ambiente do zero") |
| M5 | menor | 4, 15 | `media-relay ×2` vale para `docker compose up`; com `make start` (modo voz) são dois serviços, `media-relay` e `media-relay-2`, de 1 réplica cada (`compose.voice.yml`) | Uma frase na nota de fala | aplicado (nota no slide 15 do roteiro) |
| M6 | menor | Plano B, passo 4 | `make test` roda Go e `dotnet test` (em contêiner, se não houver SDK local) e leva minutos; não serve como recuperação ao vivo | Marcar como opcional, fora do tempo | aplicado |
| M7 | menor | 1 | `DESIGN.md` 4.1 pede a miniatura do diagrama completo na coluna direita da capa; o plano pede só os 3 serviços e o hexágono | Escolher uma das duas na construção | aplicado (miniatura só com os 3 serviços e o hexágono) |
| M8 | menor | 13 | A ilustração usa `taken 10/11` (capacidade de F5), mas o padrão é `MAX_PARTICIPANTS=10` (`docker-compose.yml:41`) | Usar números genéricos ou citar F5 | aplicado |
| M9 | menor | 12, 22 | D19 e D20 constam como "aguardando revisão" em `ARCHITECTURE.md` §13 | Já perguntado em `QUESTIONS.md` | aplicado: D19–D22 aprovadas em 2026-11-26 |
| M10 | menor | 3, 7, 11 | Compensação e fan-out aparecem no slide 3 e de novo nos slides 7 e 11. É aceitável como zoom, mas o slide 3 deve ser só o mapa | Fala curta no slide 3 (45 s) | aplicado (fala de 45 s) |

## Ritmo e narrativa

- Ordem do plano: problema/tema (1–3) → arquitetura (4–10) → garantias (11–18) → falhas e demo (19–20) → decisões
  (21) → limites (22) → fechamento (23). Segue a história pedida, sem repetição além de M10.
- Ritmo: 835 s de fala em 23 slides, média de 36 s (~79 palavras por slide a 130 palavras/min). Isso é viável se a
  tela tiver só o que a regra de densidade permite e o resto ficar nas notas.
- Durações da demo (`fN.sh`, estimadas pela leitura dos scripts, a medir no ensaio): F1 ~30 s, F2 ~60–75 s, F3 ~30 s,
  F4 ~2,5–3 min, F5 ~30 s, F6 ~90 s, F7 ~40 s, F8 ~40 s. Só F4 passa da janela; tem roteiro próprio.

## Risco da demo e plano B

- O plano B (logs gravados por `make test_caseN | tee`, painel "Saída gravada" em cada apêndice, recuperação por
  `make stop && make start`) é adequado. Depende de rodar os 8 cenários e o script surpresa antes, com os trechos
  colados no deck (`[PREENCHER]`).
- Pré-requisitos agora listados no slide 19: `make build` e `make start` antes; Docker, make, curl, jq, `uuidgen`;
  Go e python3 para os scripts do audit-vault e o script surpresa; clone em diretório `larpspeak`.

## O que fica para o responsável

Perguntas novas em `QUESTIONS.md` (marcadas `[revisão]`):

1. Rodar `scripts/demo/fN.sh` ao vivo conta como "provocar manualmente" (`txt:120`)?
2. Com F4, pode-se seguir para os slides seguintes enquanto o script espera a DLQ?
3. A parte do audit-vault (`audit-fN.sh`) é mostrada ao vivo ou só se pedida?
4. O tempo de perguntas conta dentro dos 20 min?
5. Corrigir `ARCHITECTURE.md` §5.3 (linha de log do Go durante a queda do broker)?
6. Medir no ensaio a duração de cada `fN.sh` e do script surpresa?

Também continuam abertas as perguntas anteriores do plano e do design (nomes, tema registrado, URL do repositório,
D19–D22, fontes, projetor, máquina da demo, evidências gravadas).
