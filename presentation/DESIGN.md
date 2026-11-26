# Larpspeak — guia visual dos slides

Os slides (um único arquivo HTML, 1920×1080, pt-BR) reproduzem a identidade visual da interface `web/`.
Todo valor abaixo vem do código do frontend, com `arquivo:linha`. Quando um valor não existe no frontend, ele é
**derivado** de um token existente, e a regra da derivação aparece ao lado. O que não dá para decidir pelo código
fica marcado como **[PREENCHER]**, com a pergunta correspondente em `presentation/QUESTIONS.md`.

---

## 1. Fonte da verdade

Arquivos lidos (todos em `web/`):

| Arquivo | O que define |
|---|---|
| `index.html` | idioma `pt-BR` (:2), `theme-color #07090C` (:6), favicon SVG inline (:8), fontes via Google Fonts (:9-11) |
| `src/index.css` | tokens `@theme` (:3-18), base do `body` (:20-26), seleção e foco (:27-28), `.scanlines` (:31-35), `.grain` (:37-41), `.glow-signal` (:43), movimento reduzido (:45-47) |
| `src/main.tsx` | `MotionConfig reducedMotion="user"` (:10) |
| `src/App.tsx` | transição entre telas (:32-40), tela de carregamento (:42-47) |
| `src/components/Radio.tsx` | fundo `Backdrop` (:5-14), régua de sintonia `Band` (:26-58), leitura de frequência `Readout` (:61-75) |
| `src/components/Icons.tsx` | ícones SVG inline, traço 1.7 (:4-7) |
| `src/components/TopBar.tsx` | barra superior (:11-37) |
| `src/components/CallParts.tsx` | `ParticipantTile` (:10-66), `Control` (:70-95), `Drawer` (:99-122), `SettingsPanel` (:126-180), `OpsPanel` (:184-219) |
| `src/components/InviteInput.tsx` | chips e campo (:33-60) |
| `src/components/IncomingToast.tsx` | cartão flutuante com vidro e brilho (:12-46) |
| `src/screens/Auth.tsx` | composição de tela de entrada (:39-128), campo (:142-162) |
| `src/screens/Lobby.tsx` | grade em duas colunas, títulos, lista (:48-172) |
| `src/screens/Call.tsx` | cabeçalho de frequência, status, banner, barra de controles (:195-321) |
| `src/strings.ts` | vocabulário de rádio (:3-6, :45-53, :70-74) |
| `src/frequency.ts` | callId vira frequência `100.000–199.999` MHz (:3-8) |
| `package.json`, `vite.config.ts` | Tailwind 4.3.4, Motion 14.9.0, React 19.3.1, Vite 8.3.5; plugin `@tailwindcss/vite`, sem config de tema extra |
| `dist/assets/index-*.css` | só para confirmar os valores padrão do Tailwind 4 usados (escala de texto, tracking, blur, easing) |

Não existem `web/public/` nem `web/src/assets/`: não há arquivos de fonte, logotipo ou imagem no repositório. O único
"logotipo" é o favicon SVG inline (`index.html:8`).

---

## 2. Tokens

### 2.1 Paleta (só tema escuro)

O frontend tem **um único tema, escuro**. Não há modo claro nem `prefers-color-scheme` (`index.css:3-18`).

| Token | Valor | Uso no frontend |
|---|---|---|
| `--color-bg` | `#07090c` | fundo da página (`index.css:22`), texto sobre botão preenchido (`IncomingToast.tsx:37`) |
| `--color-surface` | `#0d1117` | painéis e cartões, quase sempre translúcidos: `/70` tile (`CallParts.tsx:28`), `/60` item de lista (`Lobby.tsx:152`), `/95` drawer e toast (`CallParts.tsx:111`, `IncomingToast.tsx:22`) |
| `--color-raised` | `#121823` | fundo do avatar parado (`CallParts.tsx:46`) |
| `--color-line` | `#1c2430` | todas as bordas e divisórias (`TopBar.tsx:11`, `Lobby.tsx:113`), trilho de nível (`CallParts.tsx:60`) |
| `--color-text` | `#e6edf3` | texto principal (`index.css:23`) |
| `--color-dim` | `#8593a3` | texto secundário, rótulos, legendas (`Auth.tsx:146`) |
| `--color-signal` | `#5cffc8` | acento: ativo, "no ar", foco, seleção, botão principal, quem fala (`Call.tsx:191`, `index.css:27-28`) |
| `--color-amber` | `#ffb547` | aguardando / procurando / reconectando (`Auth.tsx:50-51`, `TopBar.tsx:23`, `Call.tsx:226`) |
| `--color-red` | `#ff4d5e` | erro, encerrado, mudo, sair (`Auth.tsx:107`, `CallParts.tsx:55`, `CallParts.tsx:79`) |
| `--color-blue` | `#6cb6ff` | brilho de fundo (`Radio.tsx:9`) e eventos SSE no log (`CallParts.tsx:211`) |

Semântica de estado já usada no código, que os slides devem manter:

- status da chamada: `ACTIVE` → signal, `PENDING` → amber, demais (`FAILED`/`ENDED`) → red (`CallParts.tsx:195`);
- fase da tela: `live` → signal; `closed`/`error` → red; o resto (`tuning`, `connecting`, `rerouting`) → amber
  (`Call.tsx:190-191`);
- origem de evento no log: `sse` → blue, `rtc` → signal, `http` → amber (`CallParts.tsx:211`);
- medidor de nível: segmentos signal, amber acima de 16/24, red acima de 20/24 (`CallParts.tsx:156`).

Transparências: o frontend usa modificadores do Tailwind 4 (`bg-signal/10`, `border-signal/60` etc.), que compilam
para `color-mix(in oklab, <cor> N%, transparent)`. Nos slides use a mesma forma (`color-mix(in oklab, …)`), nunca um
hex novo.

### 2.2 Bloco pronto para colar

```css
:root {
  /* Paleta — web/src/index.css:4-13 (valores exatos) */
  --color-bg: #07090c;
  --color-surface: #0d1117;
  --color-raised: #121823;
  --color-line: #1c2430;
  --color-text: #e6edf3;
  --color-dim: #8593a3;
  --color-signal: #5cffc8;
  --color-amber: #ffb547;
  --color-red: #ff4d5e;
  --color-blue: #6cb6ff;

  /* Fontes — web/src/index.css:15-17 (pilhas exatas, incluindo fallback) */
  --font-display: "Oxanium", ui-sans-serif, system-ui, sans-serif;
  --font-sans: "Manrope", ui-sans-serif, system-ui, sans-serif;
  --font-mono: "JetBrains Mono", ui-monospace, monospace;

  /* Pesos usados — index.html:11 carrega só estes */
  --weight-display-light: 300;   /* Readout e títulos: font-light (Auth.tsx:49, Lobby.tsx:59) */
  --weight-display-medium: 500;
  --weight-display-bold: 700;    /* carregado, não usado em nenhum componente */
  --weight-sans: 400;            /* também 500 e 600 carregados */
  --weight-mono: 400;            /* também 500 carregado */

  /* Letter-spacing — valores literais das classes tracking-[...] */
  --tracking-brand: 0.4em;    /* marca "Larpspeak" (TopBar.tsx:12, Auth.tsx:46) */
  --tracking-label: 0.3em;    /* rótulos de seção/campo (Auth.tsx:146, CallParts.tsx:114) */
  --tracking-status: 0.25em;  /* linha de status e botão principal (Call.tsx:206, Auth.tsx:117) */
  --tracking-button: 0.2em;   /* abas, botões secundários (Auth.tsx:72, Lobby.tsx:129) */
  --tracking-control: 0.15em; /* rótulo de controle e botões do toast (CallParts.tsx:92, IncomingToast.tsx:30) */
  --tracking-wide: 0.025em;   /* títulos display (Lobby.tsx:59) — Tailwind 4 padrão */
  --tracking-wider: 0.05em;   /* iniciais do avatar (CallParts.tsx:46) */
  --tracking-widest: 0.1em;   /* selo "você" (CallParts.tsx:53) */

  /* Efeitos */
  --glow-text: 0 0 18px color-mix(in oklab, var(--color-signal) 55%, transparent); /* index.css:43 */
  --glow-needle: 0 0 12px var(--color-signal);          /* Radio.tsx:45 (shadow-[0_0_12px] + cor do tom) */
  --glow-underline: 0 0 8px var(--color-signal);        /* Auth.tsx:76 */
  --glow-card: 0 0 40px color-mix(in oklab, var(--color-signal) 10%, transparent); /* IncomingToast.tsx:22 */
  --blur-glass: 8px;   /* backdrop-blur (CallParts.tsx:111, Call.tsx:289); confirmado no CSS compilado */
  --blur-blob: 64px;   /* blur-3xl dos brilhos de fundo (Radio.tsx:8-9) */
  --exit-blur: 6px;    /* saída de tela (App.tsx:38) */

  /* Movimento — Tailwind 4 padrão (confirmado em dist) e Motion */
  --ease-ui: cubic-bezier(0.4, 0, 0.2, 1);       /* transition-colors padrão, 150 ms */
  --duration-ui: 150ms;
  --ease-out: cubic-bezier(0, 0, 0.58, 1);       /* 'easeOut' do Motion (Auth.tsx:43) */
  --ease-in-out: cubic-bezier(0.42, 0, 0.58, 1); /* 'easeInOut' do Motion (Radio.tsx:53) */

  /* Escala de slide — DERIVADA: largura do canvas / largura máxima do conteúdo no frontend
     (max-w-6xl = 72rem = 1152px, Lobby.tsx:48). 1920 / 1152 = 1.6667 */
  --k: 1.6667;
  --pad-x: 80px;  /* px-12 = 48px (Lobby.tsx:48) × k */
  --pad-y: 64px;  /* DERIVADO: py-10 = 40px (Call.tsx:199) × k ≈ 67 → arredondado para múltiplo de 4 */
  --gap-col: 80px; /* gap-12 / pl-12 = 48px (Lobby.tsx:50, :113) × k */

  /* Raio: o frontend não arredonda painéis, botões nem campos. Só pontos, avatar e agulha são rounded-full. */
  --radius: 0;
}

html, body { background: var(--color-bg); color: var(--color-text); font-family: var(--font-sans);
  -webkit-font-smoothing: antialiased; }                                             /* index.css:20-26 */
::selection { background: color-mix(in oklab, var(--color-signal) 30%, transparent); } /* index.css:27 */
:focus-visible { outline: 2px solid var(--color-signal); outline-offset: 3px; }      /* index.css:28 */
.glow-signal { text-shadow: var(--glow-text); }                                     /* index.css:43 */
```

### 2.3 Texturas e fundo (copiar do frontend)

`Backdrop` (`Radio.tsx:5-14`) fica atrás de todo slide, fixo, sem interação:

1. brilho signal: elipse `80vh × 80vw`, `left:-25%`, `top:-33%`, `rounded-full`, cor signal a 5%, `blur(64px)`;
2. brilho blue: elipse `60vh × 50vw`, `bottom:-33%`, `right:0`, cor blue a 4%, `blur(64px)`;
3. `.scanlines` (`index.css:31-35`): `repeating-linear-gradient(to bottom, rgb(255 255 255 / 0.025) 0 1px, transparent 1px 3px)`,
   animação `roll 9s linear infinite` até `background-position: 0 300px`;
4. `.grain` (`index.css:37-41`): SVG `feTurbulence` inline em data URI (copiar a string exata), `opacity: .06`,
   `mix-blend-mode: overlay`.

Não há gradientes lineares de cor, imagens nem ilustrações. Os únicos gradientes são esses brilhos desfocados e as
scanlines. Rolagem das scanlines em projetor: **paradas** (decisão do responsável). Use as scanlines
**paradas**, que é o comportamento do frontend com movimento reduzido (`index.css:45-47`).

---

## 3. Tipografia e hierarquia

### 3.1 Papéis das três famílias

| Família | Papel no frontend | Onde |
|---|---|---|
| **Oxanium** (display) | números de frequência, títulos, botões principais, iniciais do avatar | `Radio.tsx:71`, `Lobby.tsx:59`, `Auth.tsx:117`, `CallParts.tsx:46` |
| **Manrope** (sans) | texto corrido: subtítulos, descrições, ajuda | `index.css:24`, `Auth.tsx:55`, `Lobby.tsx:60` |
| **JetBrains Mono** (mono) | rótulos em CAIXA ALTA espaçada, status, identificadores, campos, log, tabelas | `Auth.tsx:146`, `Call.tsx:205`, `CallParts.tsx:193` |

Assinatura tipográfica: **Oxanium light (300) grande + JetBrains Mono pequeno em caixa alta com tracking 0.3em**.
Títulos nunca são negrito. Números usam `tabular-nums` (`Radio.tsx:71`).

### 3.2 Carregamento das fontes

O frontend carrega as três famílias do Google Fonts (`index.html:9-11`):
`Oxanium:wght@300;500;700`, `Manrope:wght@400;500;600`, `JetBrains+Mono:wght@400;500`, `display=swap`.

Os slides **não podem depender de rede**. Opções:

- embutir os arquivos `woff2` (licença SIL OFL nas três) como `@font-face` com `src: url(data:font/woff2;base64,…)`,
  só com os pesos acima. **Decidido: embutir.** Os arquivos vêm do Google Fonts (os mesmos que o frontend carrega),
  baixados uma vez na construção do deck; o HTML pode crescer algumas centenas de KB;
- sem os arquivos, valem exatamente os fallbacks do frontend (`index.css:15-17`): `ui-sans-serif, system-ui,
  sans-serif` para display e sans; `ui-monospace, monospace` para mono. Neste caso a identidade perde a Oxanium, que é
  a fonte mais reconhecível.

### 3.3 Escala para o canvas 1920×1080

Regra: tamanho no frontend × `k = 1.6667`, arredondado, com **piso de 24px** para qualquer texto que a plateia deva
ler (projetor). Onde o piso altera o valor, a tabela indica.

| Papel no slide | Origem no frontend | Família / peso | Tamanho slide | Line-height | Tracking | Caixa |
|---|---|---|---|---|---|---|
| Número herói (capa, divisor) | `Readout text-8xl` 96px (`Auth.tsx:49`) | display 300, `tabular-nums`, signal + glow | **160px** | 1 | normal | — |
| Número grande secundário | `text-7xl` 72px (`Call.tsx:202`) | display 300 | **120px** | 1 | normal | — |
| Título do slide | `h1 text-4xl` 36px (`Lobby.tsx:59`) | display 300 | **60px** | 1.11 (2.5/2.25) | 0.025em | normal |
| Subtítulo / título de coluna | `h2 text-2xl` 24px (`Lobby.tsx:117`) | display 300 | **40px** | 1.33 | 0.025em | normal |
| Texto de apoio (lead) | `text-lg` 18px (`Auth.tsx:55`) | sans 400, dim | **30px** | 1.56 | normal | normal |
| Corpo / bullets | `text-base` 16px (padrão do body) | sans 400, text | **28px** (16×k = 26.7 → arredondado acima) | 1.5 | normal | normal |
| Eyebrow / rótulo de seção | `text-[11px]` (`Auth.tsx:146`) | mono 400, dim | **24px** (piso; 11×k = 18) | 1.33 | 0.3em | CAIXA ALTA |
| Marca na barra superior | `text-xs` 12px (`TopBar.tsx:12`) | mono 400, dim | **24px** (piso; 12×k = 20) | 1.33 | 0.4em | CAIXA ALTA |
| Linha de status (com ponto) | `text-xs` (`Call.tsx:205-206`) | mono 400, cor do estado | **24px** (piso) | 1.33 | 0.25em | CAIXA ALTA |
| Código / log / tabela | `text-xs`–`text-sm` (`CallParts.tsx:193`) | mono 400 | **24px** (14×k = 23 → piso) | 1.5 | normal | normal |
| Legenda / fonte / nota | `text-xs` dim (`Auth.tsx:160`) | sans 400 ou mono, dim | **24px** (piso) | 1.33 | normal | normal |
| Rótulo de nó no diagrama | `font-display text-xl` 20px (`Lobby.tsx:155`) | display 500 | **32px** | 1.2 | 0.025em | normal |
| Sub-rótulo de nó / aresta | `text-[10px]` mono (`CallParts.tsx:92`) | mono 400 | **22px** — exceção ao piso de 24 só dentro de diagramas densos (decidido; conferir no ensaio) | 1.3 | 0.15em | CAIXA ALTA |
| Régua decorativa (números da `Band`) | `text-[10px]` dim/70 (`Radio.tsx:39`) | mono | 17px (10×k), decorativo, não precisa ser lido | — | — | — |

Tamanho de texto mínimo por slide: no máximo **5 tamanhos** (o frontend usa poucos por tela: `Lobby` usa 4xl, 2xl,
base, sm, 11px).

---

## 4. Layouts de slide

Comum a todos os layouts:

- canvas `1920×1080`, fundo `--color-bg` + `Backdrop` (seção 2.3);
- **barra superior** igual ao `TopBar` (`TopBar.tsx:11-37`): altura 88px, `padding: 0 80px`, `border-bottom: 1px solid
  var(--color-line)`; à esquerda a marca `LARPSPEAK` (mono 24px, tracking 0.4em, dim); à direita, em mono 24px dim,
  o nome da seção e o contador `07 / 18` com o número atual em signal (como `signedAs <userId>` em signal,
  `TopBar.tsx:30-31`). Na capa a barra não aparece (a tela `Auth` também não tem `TopBar`);
- área útil: `x 80–1840`, `y 88+64 = 152` até `1080−64 = 1016` (864px de altura);
- alinhamento à esquerda; nada centralizado exceto o carregamento (`App.tsx:42`);
- **colunas separadas por linha vertical**, não por cartões: `border-left: 1px solid var(--color-line)` +
  `padding-left: 80px` (`Lobby.tsx:113`, `Auth.tsx:63`);
- sem cantos arredondados, sem sombras de elevação; só brilho (`--glow-*`) em elementos ativos.

### 4.1 Capa (título)

Composição da tela `Auth` (`Auth.tsx:39-57`), grade `1.2fr 1fr`, `gap 107px` (gap-16 = 64 × k), centrada na
vertical:

- coluna esquerda, de cima para baixo, espaçamento 67px (space-y-10 × k):
  1. eyebrow `LARPSPEAK` — mono 24px, tracking 0.4em, dim;
  2. leitura herói: `Larpspeak` ou uma frequência (ex.: `frequency()` de um callId real da demo) em display 300 160px,
     signal + `glow-signal`;
  3. linha de status amber com ponto pulsante (`Auth.tsx:50-53`): ponto 10px (size-1.5 × k) amber `animation: pulse`,
     texto mono 24px tracking 0.25em: `ARQUITETURA · SISTEMAS DISTRIBUÍDOS`;
  4. lead em sans 30px dim, largura máx. 640px (max-w-sm × k): nomes do grupo (Nícolas Paterno · William Barboza ·
     Gustavo Amaro · Gustavo Luchini) e disciplina; o título é o nome do projeto (sem professor nem data);
  5. `Band` (régua 100–200 com agulha) com largura 1067px (max-w-xl × k), tom amber varrendo, ou signal travada.
- coluna direita: `border-left` line + `padding-left 80px`, contendo a miniatura do diagrama base **só com os três
  serviços e o hexágono `call-events`** (escolha alinhada ao `PLAN.md`, slide 1), sem rótulos de aresta.

### 4.2 Divisor de seção

- eyebrow mono 24px tracking 0.3em dim: `SEÇÃO 02`;
- número herói display 300 160px signal glow: `02` ou a frequência da seção (ex.: `120.000`), seguido de `MHz` mono
  24px dim alinhado pela base (`Call.tsx:201-203`);
- título da seção display 300 60px text;
- `Band` em largura total (1760px) com a agulha **travada** na posição da seção (posição = `(n / total) × 100%`),
  tom signal;
- à direita/abaixo, miniatura do diagrama com **só a fatia da seção em foco** (seção 5.5).

### 4.3 Diagrama dominante

- eyebrow (24px) + título (60px) no topo da área útil, ocupando ~140px;
- diagrama SVG em `1760 × 700`, `viewBox` igual ao tamanho em px (1 unidade = 1px);
- legenda na base (seção 5.6), linha de 48px, separada por `border-top` line.

### 4.4 Diagrama + 3 bullets

Grade do `Lobby` (`Lobby.tsx:50`): `grid-template-columns: 1.15fr 1fr`, `gap 80px`.

- coluna esquerda: diagrama (fatia), `~940 × 760`;
- coluna direita, com `border-left` line e `padding-left 80px`: eyebrow, título 40px, e **exatamente 3 itens**,
  espaçados 53px (space-y-8 × k). Cada item: índice mono 24px signal (`01`, `02`, `03`) acima ou à esquerda; texto
  sans 28px text; detalhe opcional em mono 24px dim (ex.: `relay.go:176-219`). Sem marcadores de bolinha;
- cada bullet pode acender o nó correspondente no diagrama (mesma cor de destaque, seção 5.5).

### 4.5 Tabela de comparação

Estilo do `OpsPanel` (`CallParts.tsx:186-204`): lista de definição, não grade com bordas.

- cabeçalho de colunas: mono 24px tracking 0.3em dim, caixa alta;
- cada linha: `border-bottom: 1px solid color-mix(in oklab, var(--color-line) 60%, transparent)`, `padding 13px 0`
  (py-2 × k); 1ª coluna dim, demais text; tudo mono 24px;
- valores de estado coloridos com a semântica da seção 2.1 (ex.: `ACTIVE` signal, `PENDING` amber, `FAILED` red);
- máximo de 6 linhas × 4 colunas;
- uma miniatura de diagrama (≈ 480px de largura) à direita ou acima, mostrando onde cada linha acontece.

### 4.6 Código / log

Estilo do `Drawer` + `OpsPanel` (`CallParts.tsx:111-117`, `:205-215`):

- painel: fundo surface a 95%, `border: 1px solid var(--color-line)`, `backdrop-filter: blur(8px)`, sem raio;
- cabeçalho do painel: `padding 27px 33px` (py-4/px-5 × k), `border-bottom` line, título mono 24px tracking 0.3em
  dim (nome do arquivo ou `EVENTOS`), à direita a referência `arquivo:linha` em mono dim;
- corpo: mono 24px, line-height 1.5, máximo **12 linhas**;
- linhas de log em grade `auto auto 1fr`, gap 13px: hora em dim a 70%, tipo colorido pela origem (`sse`/mensagem →
  blue, `rtc`/mídia → signal, `http` → amber), dados em dim;
- realce de linha: texto signal + `glow-signal`, sem fundo; linhas fora de foco em opacidade 0.4.
- Não há realce de sintaxe no frontend: no código, use só text, dim (comentários) e signal (o trecho em foco).
- ao lado (coluna 1fr), fatia do diagrama mostrando o serviço dono do código.

### 4.7 Demo / terminal

Composição da tela `Call` (`Call.tsx:199-230`):

- cabeçalho: frequência da chamada da demo em display 300 120px + `MHz`; abaixo, linha de status com ponto (signal
  `NO AR`, amber pulsante `SINTONIZANDO`/`REDIRECIONANDO SINAL`, red `ENCERRADO`) — textos de `strings.ts:45-53`;
- `Band` à direita do cabeçalho com a agulha na frequência e no tom da fase;
- banner opcional de falha (`Call.tsx:226`): `border 1px amber a 40%`, fundo amber a 5%, mono 24px amber, padding
  `20px 27px`, ex.: `O relay caiu. Redirecionando o sinal…`;
- painel de terminal (estilo 4.6) com o comando e a saída; prompt `$` em dim, comando em text, saída relevante em
  signal (ok) / red (falha);
- fatia do diagrama mostrando o componente que a demo derruba ou observa, com o nó afetado em red (seção 5.5).

---

## 5. Diagramas (SVG inline)

Base semântica: `ARCHITECTURE.md` §1.1 (mermaid). As formas abaixo seguem as formas do mermaid; cores e traços vêm
da paleta e da semântica de origem do log (`CallParts.tsx:211`).

### 5.1 Regras gerais

- SVG inline, `viewBox` em px do canvas, `font-family` herdada das variáveis; textos como `<text>`, nunca imagem;
- traço base 2px; cantos retos (`rx=0`) em tudo, exceto clientes (pílula) e pontos;
- preenchimento sempre translúcido sobre o fundo (`surface` a 70%, como o tile, `CallParts.tsx:28`);
- **bordas de nó em `--color-dim`, não em `--color-line`**. DERIVADO: `line` sobre `bg` tem contraste 1.28:1 e some
  no projetor (seção 7). `line` fica só para divisórias e grades de fundo;
- grade de alinhamento: 8px; distância mínima entre nós 64px;
- rótulos de aresta: mono 22–24px dim, caixa normal para nomes de mensagem (`RelayRequested`), fundo `--color-bg`
  atrás do texto (retângulo com padding 6px) para não cruzar linhas.

### 5.2 Nós por tipo

| Tipo (mermaid) | Forma | Traço | Preenchimento | Texto |
|---|---|---|---|---|
| **Serviço Go** (`call-signaling`, `media-relay`) — `subgraph` | retângulo 320×120 | 2px dim | surface 70% | nome em display 500 32px text; abaixo, selos (estilo "você", `CallParts.tsx:53`): borda 1px dim, padding 0 10px, mono 22px tracking 0.1em dim caixa alta: `GO 1.27`, `×2` |
| **Serviço C#** (`audit-vault`) | igual ao Go | 2px dim | surface 70% | selo `C# · .NET 10`, `×2`. A linguagem é diferenciada **só pelo selo**, porque o frontend não tem cor por linguagem (decidido: só o selo) |
| **web / nginx** | retângulo 280×96 | 1.5px dim, tracejado `6 6` (não tem regra de negócio, `ARCHITECTURE.md` §1) | nenhum | nome display 500 28px dim; selo `NGINX 1.29` |
| **Cliente** (Navegador, voice-client, operador) — `([ ])` | pílula (`rx = altura/2`, como `rounded-full` do avatar, `CallParts.tsx:46`) 240×72 | 1.5px dim | raised | mono 24px text |
| **Fila SQS** — `[[ ]]` | retângulo 300×64 com duas barras verticais internas a 12px das bordas | 2px blue a 60% | blue a 5% (padrão banner: borda /40, fundo /5, `Call.tsx:226`) | mono 22px text, nome da fila (`relay-requests`); eyebrow `SQS` mono blue |
| **DLQ** (quando aparecer) | igual à fila | 2px red a 50%, tracejado `6 6` | red a 5% | `<fila>-dlq` |
| **Tópico SNS** — `{{ }}` | hexágono achatado 300×80 | 2px blue | blue a 10% | mono 22px text `call-events`; eyebrow `SNS` blue |
| **DynamoDB** — `[( )]` | cilindro 220×96 (elipse do topo 20px de altura) | 1.5px dim | raised | eyebrow `DYNAMODB` mono 22px dim; nome mono 22px text (`signaling-*`) |
| **S3** — `[( )]` | cilindro, mesmo tamanho | 1.5px dim | raised | `S3` + `larpspeak-audio` |
| **Secrets Manager** — `[( )]` | cilindro, mesmo tamanho | 1.5px dim | raised | `SECRETS MANAGER` + nome do segredo |
| **MiniStack** (moldura, se mostrada) | retângulo que envolve SQS/SNS/DynamoDB/S3/SM | 1px line, tracejado `4 8` | nenhum | eyebrow mono 24px tracking 0.3em dim `MINISTACK · AWS :4566` |

Cada serviço desenha seu banco **colado abaixo dele** (aresta curta), reforçando "cada serviço tem as suas tabelas"
(`ARCHITECTURE.md` §1.1, `CS --- CSDB`).

### 5.3 Arestas

| Tipo | Exemplo (§1.1) | Cor | Traço | Ponta |
|---|---|---|---|---|
| **HTTP síncrono** | `CS → MR POST /webrtc`, `MR → CS GET /auth/public-key`, navegador → web | amber (http = amber, `CallParts.tsx:211`) | 2px contínuo | seta cheia, uma ponta |
| **Mensagem assíncrona** (SQS/SNS) | `CS → relay-requests → MR`, `T → Q1 → MR` | blue (sse/evento = blue, `CallParts.tsx:211`) | 2px tracejado `10 6` — DERIVADO: o mermaid usa linha contínua; o tracejado separa de HTTP sem depender só da cor | seta cheia |
| **Mídia UDP / WebRTC** | `browser <-.-> MR`, `vc <-.-> MR` | signal (rtc = signal) | 2.5px pontilhado `1 8`, `stroke-linecap: round` (mermaid `-.->`) | seta nas duas pontas |
| **Acesso a banco** | `CS --- CSDB`, `AV → S3`, `CS → SM` | dim | 1.5px contínuo | sem ponta (`---`); com ponta só onde o mermaid tem seta (`AV → S3`, leitura do SM) |
| **SSE (servidor → navegador)** | dentro de "HTTP /api/* + SSE" | amber, como no mermaid (uma aresta só) | — | — |

Setas: `<marker>` triângulo 10×10 com `fill` = cor da aresta. Curvas: só ortogonais (cotovelos de 90°) ou retas;
sem Bézier livre (o frontend é todo em retas e ângulos retos).

### 5.4 Selos e anotações

- números de requisito/falha (`R1`, `F2`) como selo mono 22px no estilo "você": borda 1px, cor do estado (signal =
  atende, amber = degradado, red = falha);
- marca de transação (outbox / `TransactWriteItems`): selo `TX` signal junto ao banco;
- quedas (demo F2/F7): nó com borda red, `×` (`Icons.tsx:39-41`) red no canto superior direito, como os ícones de mudo
  no tile (`CallParts.tsx:55-58`).

### 5.5 Fatia: foco e esmaecimento

Técnica tirada do próprio frontend:

- **fora de foco**: `opacity: 0.4` no grupo `<g>` (mesmo valor do controle desabilitado, `Lobby.tsx:63`, e do
  `Readout` sem sinal, `text/40`, `Radio.tsx:71`). O texto esmaecido fica em 3.34:1, ainda legível como contexto;
- **em foco**: borda do nó em signal (como o tile de quem fala, `border-signal/70`, `CallParts.tsx:28`), fundo
  signal a 10% (`bg-signal/10`, `CallParts.tsx:46`), nome em signal + `filter: drop-shadow(0 0 18px
  color-mix(in oklab, var(--color-signal) 55%, transparent))` (equivalente SVG do `glow-signal`);
- arestas do caminho em foco mantêm a cor do seu tipo (seção 5.3) e ficam em opacidade 1; o resto fica em 0.4;
- foco de falha: mesma técnica, com red no lugar de signal;
- **zoom**: a fatia é o mesmo SVG com outro `viewBox` (recorte), não um desenho novo. As posições dos nós não mudam
  entre slides — o público reconhece o mapa.

### 5.6 Legenda

Faixa na base do diagrama, `border-top` line, altura 48px, itens em linha com gap 48px:

- amostra de 48px de cada aresta (HTTP amber contínua, mensagem blue tracejada, mídia signal pontilhada, banco dim);
- miniatura 24px de cada forma usada no slide;
- texto mono 22–24px, caixa alta, tracking 0.15em, dim: `HTTP`, `MENSAGEM SQS/SNS`, `MÍDIA UDP/WEBRTC`, `BANCO`.

Só mostrar na legenda os tipos presentes no slide.

---

## 6. Movimento

O frontend usa Motion com `reducedMotion="user"` (`main.tsx:10`). Os slides devem reproduzir os mesmos padrões em
**CSS puro** (transições e `@keyframes`), sem a biblioteca Motion e sem nenhum recurso de rede.

| Padrão | Valores no frontend | Uso no slide |
|---|---|---|
| Troca de tela | entra `opacity 0→1`; sai `opacity→0` + `filter: blur(6px)`; 0.3s (`App.tsx:36-39`) | **transição entre slides** (única permitida) |
| Entrada de bloco | `opacity 0→1`, `translateY(12–14px)→0`, 0.5–0.7s, easeOut; 2º bloco com atraso 0.1–0.15s (`Auth.tsx:41-62`, `Lobby.tsx:53-112`) | entrada das colunas de um slide |
| Entrada lateral | `translateX(16px)→0`, 0.7s, atraso 0.15s (`Auth.tsx:60-62`) | coluna direita da capa |
| Item de lista | `opacity 0→1`, `translateX(-12px)→0` (`Lobby.tsx:148-150`) | bullets que aparecem um a um |
| Tile | `opacity 0→1`, `translateY(16px)→0`, mola 260/26 (`CallParts.tsx:23-26`) | nó que aparece no diagrama: usar `cubic-bezier(.4,0,.2,1)` 0.4s como aproximação — **mola não existe em CSS** |
| Agulha da `Band` | varre `6%↔94%` em 7s easeInOut infinito; trava com mola 120/14 (`Radio.tsx:47-53`) | capa (varrendo) e divisores (travada) |
| Readout sem sinal | dígitos aleatórios a cada 90ms (`Radio.tsx:66`) | **não usar** em slide (distrai, precisa de JS) |
| Pulso | `pulse 2s cubic-bezier(.4,0,.6,1) infinite`, opacidade 50% no meio (Tailwind, `Call.tsx:207`) | ponto de status amber |
| Ping | `ping 1s cubic-bezier(0,0,.2,1) infinite`, escala 2 e opacidade 0 (`IncomingToast.tsx:24`) | evento chegando numa fila/nó (uma vez, não infinito) |
| Anéis de fala | 3 anéis, escala 1→2.1, opacidade .7→0, 1.6s, atraso `i×0.53s` (`CallParts.tsx:33-42`) | nó em foco "vivo" (media-relay com áudio) |
| Hover/estado | `transition-colors 150ms cubic-bezier(.4,0,.2,1)` | troca de foco entre fatias |
| Scanlines | `roll 9s linear infinite` (`index.css:33`) | ver 2.3 — parado até decisão |

Regras:

- duração máxima de uma transição: 0.7s; nada de zoom de câmera, giro, salto ou slide lateral de página inteira;
- `@media (prefers-reduced-motion: reduce)`: desligar scanlines, pulso, ping, anéis e a varredura da agulha
  (`index.css:45-47`, `Radio.tsx:48`, `CallParts.tsx:32`);
- animação de mensagem percorrendo uma aresta (ponto que anda) só com `stroke-dashoffset` em CSS, na cor da aresta.
  DERIVADO dos traços da seção 5.3; o frontend não tem esse padrão.

Sem dependência externa: nada de Google Fonts, CDN, imagens remotas ou scripts externos. Fontes: seção 3.2.

---

## 7. Acessibilidade e legibilidade em projetor

Contraste WCAG 2.x calculado com os hex exatos (transparências compostas sobre `--color-bg` em sRGB; o navegador
mistura em oklab, a diferença é pequena):

| Primeiro plano | sobre `bg` #07090c | sobre `surface` #0d1117 | sobre `raised` #121823 | Leitura |
|---|---|---|---|---|
| text #e6edf3 | **16.87** | 16.02 | 15.05 | AAA |
| signal #5cffc8 | **15.75** | 14.95 | 14.05 | AAA |
| amber #ffb547 | **11.35** | 10.77 | 10.12 | AAA |
| blue #6cb6ff | **9.28** | 8.81 | 8.28 | AAA |
| dim #8593a3 | **6.36** | 6.04 | 5.67 | AA (AAA para texto ≥ 24px) |
| red #ff4d5e | **6.15** | 5.84 | 5.48 | AA (AAA para texto ≥ 24px) |
| bg sobre signal (botão cheio) | 15.75 | — | — | AAA |
| dim a 70% (≈ #5f6a76) | 3.62 | — | — | só texto grande / decorativo |
| text a 40% (fatia esmaecida, ≈ #606468) | 3.34 | — | — | contexto, não conteúdo |
| dim a 50% (≈ #464e58) | 2.36 | — | — | **não usar para texto** |
| line #1c2430 (borda) | **1.28** | 1.21 | 1.14 | **invisível em projetor**: só divisórias |

Consequências:

- texto que precisa ser lido: text, signal, amber, blue em qualquer tamanho; dim e red com ≥ 24px;
- nunca texto em dim abaixo de 70% de opacidade (o frontend usa `dim/50` e `dim/60` só em placeholders e números de
  régua, `Auth.tsx:158`, `Lobby.tsx:78`);
- bordas de nó e arestas em dim ou em cor de estado (seção 5.1), nunca só em line;
- cor nunca é o único sinal: arestas têm traço diferente por tipo (5.3); estados têm texto (`NO AR`, `FALHA`) além da
  cor, como o frontend faz (`Call.tsx:206-208`, `CallParts.tsx:27`);
- red (#ff4d5e) e signal (#5cffc8) podem se confundir para daltonismo vermelho-verde: falha sempre com `×` ou palavra;
- projetor lava pretos: o fundo #07090c vira cinza-escuro, o que **reduz** contraste de dim e red. Por isso o piso de
  24px. Testar no projetor real antes da apresentação, se houver ensaio na sala.
- o tema é só escuro, mantido pelo responsável mesmo em sala clara; não inventar tema claro.

---

## 8. Fazer / Não fazer

**Fazer**

- Usar só os 10 tokens de cor da seção 2.2 e transparências deles via `color-mix(in oklab, …)`.
- Títulos em Oxanium light; rótulos em JetBrains Mono caixa alta com tracking 0.3em; texto corrido em Manrope.
- Cantos retos em painéis, nós e botões; `rounded-full` só em pontos, pílulas de cliente e avatares.
- Separar colunas com uma linha vertical `line` + 80px de recuo.
- Manter as mesmas posições de nós em todos os slides; fatiar com `viewBox` e opacidade 0.4.
- Usar o vocabulário de rádio do produto quando couber: frequência (`147.320 MHz`), "no ar", "sintonizando",
  "redirecionando sinal" (`strings.ts:4-6`, `:45-53`, `:70`).
- Signal = ativo/ok/em foco; amber = esperando/HTTP; red = falha; blue = mensagem/evento.
- Ícones: só os de `Icons.tsx` (24×24, traço 1.7, `currentColor`, pontas arredondadas), escalados para 32–40px.
- Citar `arquivo:linha` em mono dim nos slides de código.

**Não fazer**

- Não criar cores novas (nem roxo, nem gradiente colorido, nem branco puro `#fff`).
- Não usar negrito em títulos nem Oxanium 700 (carregada, mas nenhum componente usa).
- Não usar sombra de elevação (`box-shadow` escura) nem cartões arredondados com sombra.
- Não usar `--color-line` como única borda de algo que o público deve ver.
- Não colocar texto legível abaixo de 24px (exceção em diagrama: 22px, a confirmar).
- Não usar emoji, clip-art, fotos, logotipos de AWS/Go/.NET (o frontend não usa nenhuma imagem).
- Não animar mais de um elemento em loop por slide; nada de dígitos aleatórios do `Readout` em slide.
- Não carregar nada da rede (fontes, CDN, imagens).
- Não centralizar blocos de texto (exceto número herói isolado, se necessário).
- Não usar mais de 3 bullets por slide no layout 4.4.
