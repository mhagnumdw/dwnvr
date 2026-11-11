# Glossário da conversão para en-US

Decidido pelo mantenedor em 11/11/2026: o nome en-US de cada termo do domínio,
para o mesmo conceito ter o mesmo nome no Go, na interface, na API e nos docs
(passo 1 da [conversão](conversao-en-us.md#como-em-passos-que-se-testam)).
A coluna pt-BR é a lista do `git grep` da varredura final (passo 8). No passo 7
este arquivo vira o `docs/glossary.md`, só em en-US e com a definição de cada
termo.

## Os termos que pediram decisão

| pt-BR | O que é | en-US | Observação |
| --- | --- | --- | --- |
| quadro | o frame do vídeo, e o JPEG do frame que o detector de objetos analisou | `frame` | `quadroMs` → `frameMs`, `temQuadro` → `hasFrame`, `quadros/` → `frames/` |
| olhada | uma passada do detector de objetos num pedaço | `inference` | `olhando`, na fila → `busy` |
| marca | o que a timeline mostra: onset de movimento ou marca de objeto | `mark` | o `Marcador` → `Marker`; o `Evento` gravado → `Event` (a linha, e a marca é como ela aparece) |
| onset | o instante em que a imagem mudou o bastante | `onset` | já é en-US |
| régua (da detecção) | a média e o desvio do score que a câmera aprende | `baseline` | |
| régua (da validação) | "a mesma régua da API" | sem termo | "the same validation" |
| régua (do `periodico`) | "a régua a bater" | `reference` | para não colidir com `baseline` |
| pedaço | o trecho de vídeo que vai ao detector de objetos | `clip` | `PedacosPorCamera` → `ClipsPerCamera`, `ErrPedacoRecusado` → `ErrClipRejected` |
| família | o grupo de classes do modelo | `family` | valores `person`, `vehicle`, `animal`; o `families` do alerta já é assim |
| área | o polígono desenhado na câmera | `area` | já é en-US no contrato (`areas`, `alertAreas`) |
| zona | a zona térmica do sensor, no Diagnóstico | `zone` | não é a área |
| funil | o destino de cada onset, no `/api/health` | `funnel` | fatias: `noVideo`, `dropped`, `failed`, `rejected`, `noObject`, `withObject`, `queued` |
| fila | a fila única de pedaços para o detector de objetos | `queue` | `esperando` → `waiting` |
| órfã | gravação de câmera removida | `orphan` | já existe `orphanBytes` |
| dono | a conta do `dwnvr.yaml` | `owner` | já é en-US no JSON |
| convite | o link de cadastro | `invite` | já é en-US no JSON; `/api/convite` → `/api/invite` |
| recusa (do detector de objetos) | o detector no ar que não conseguiu analisar o pedaço | `rejected` | |
| recusa (do formulário) | campo inválido (`usuarios.Recusado`, `Recusas`) | `ValidationError`, `FieldErrors` | a API já responde `{"fields": ...}` |
| recusa (da config) | a seção de SMTP preenchida que não serve | `Problem` | |
| descartado | o que a fila não aceitou | `dropped` | |
| Diagnóstico | a tela do admin | `Diagnostics` | `Health.svelte` → `Diagnostics.svelte`; a API fica `/api/health`, e `/api/health/servidor` → `/api/health/server` |
| detector de objetos | o modelo | `object detector` | nunca `detector` solto (`AGENTS.md`) |

## Os outros

| pt-BR | en-US | Observação |
| --- | --- | --- |
| achado | `Detection` | toda caixa que o modelo devolve; no texto, "raw detection" |
| detecção (a tela, a API) | `detection` | o achado que virou marca |
| piso | `minScore` | no `dwnvr-detect`, `min_score`, inclusive o parâmetro do `POST /detect` |
| mecanismo | `detectMethod` | valores `kleinberg-p` e `periodic` |
| caixa | `box` | |
| rajada | `burst` | |
| sensibilidade | `sensitivity` | |
| nível | `level` | |
| evento | `event` | |
| miniatura | `thumbnail` | |
| gravação | `recording` | |
| segmento | `segment` | |
