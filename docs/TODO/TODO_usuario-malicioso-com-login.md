# TODO - usuário malicioso com login

Registrado em 13/10/2026, ao discutir se o
[limite de tentativas do login](../configuracao.md#limite-de-tentativas-no-login) devia
valer para todas as rotas. Ainda não decidido.

**Status: não começado.**

## Por quê

Hoje o dwnvr é usado por uma família, e quem tem login é de confiança. Uma
instalação com muitos usuários pode ter um deles mal-intencionado, que use as
rotas da API para:

- **levar as gravações embora**, baixando tudo o que alcança;
- **ocupar o servidor**, repetindo as rotas caras até atrapalhar a gravação e
  as outras pessoas.

## O que já existe

- **Revogar o acesso.** Remover a pessoa ou gerar um link novo para ela
  derruba a sessão (a chave da sessão é por pessoa, `chaveDaSessao` em
  `internal/api/auth.go`).
- **O teto do export.** `maxExportSpan` (`internal/api/recordings.go`) limita
  um export a 10 min de vídeo, o mesmo que a tela de Gravações pede. Quem
  chama a API direto não consegue pedir mais que isso.
- **O log** registra login, logout e requisição recusada. Requisição aceita
  não vai para o log: não há como saber depois quem baixou o quê.
- **As rotas públicas além do login não pedem limite.** O link de convite
  (`POST /api/convite/conferir` e `POST /api/convite`) tem token de 32 bytes
  sorteados (`BytesDoToken`), conferido por SHA-256 antes de qualquer conta de
  senha: não se adivinha, e a recusa é barata. O `GET /api/session`, o
  `GET /api/version` e os arquivos da tela são baratos. Sem cookie válido, as
  rotas com sessão recusam pelo HMAC, também barato.

## Rate limit não impede levar as gravações

Um limite de requisições só atrasa quem tem acesso legítimo. Um script baixando
devagar leva o mesmo conteúdo em mais dias. Para esse caso, o que ajuda é
saber quem baixou o quê:

- **Log de auditoria** do export, dos segmentos e dos quadros de detecção, por
  usuário. O volume é o problema: a reprodução pede um segmento a cada 30 s de
  vídeo, e um log por segmento enche o log. Agregar por usuário e por câmera
  numa janela (ex.: "fulano baixou 1,2 GB da cam_x na última hora") é uma saída.

## Rate limit ajuda a não ocupar o servidor

Para quem repete as rotas caras, sim. A chave é o usuário logado, não o
endereço: atrás de um proxy, várias pessoas podem dividir um endereço.

A ideia em discussão: um limite único, que vale para todas as rotas com
sessão, e a opção de um limite próprio por rota. O login já tem o dele, bem
mais baixo e por endereço (ver
[Limite de tentativas no login](../configuracao.md#limite-de-tentativas-no-login)).
O `Limitador` de `internal/limite` serve aos dois: muda só quem o chama, o
ritmo e a rajada.
O difícil é calibrar: o limite único tem de deixar passar a maior rajada do
uso normal (a tabela abaixo), e com esse teto ele não segura muita coisa.

Alternativas a considerar:

- Um teto de exports simultâneos por usuário.

## Quanto cada rota recebe

Estimativa pela leitura do front-end em 13/10/2026, não medida. O log não
ajuda a medir, porque requisição aceita não vai para ele. Para calibrar, falta
medir num navegador real, tela por tela.

| # | Rota | Quando, por aba aberta |
| --- | --- | --- |
| 1 | `GET /api/deteccoes/quadro` | Rajada: uma por card na tela de Detecções, de 24 a 200 por página, e mais a cada rolagem. |
| 2 | `GET /api/rec/thumb` | Rajada: até 40 miniaturas da tira ao abrir um dia ou trocar de câmera, 3 em paralelo, com cache de 240 no navegador. |
| 3 | `GET /api/rec/seg` | Contínua na reprodução: um segmento a cada 30 s de vídeo (o `segmentSeconds` padrão), multiplicado pela velocidade, mais uma rajada a cada salto na timeline. É a rota de mais bytes. |
| 4 | `GET /api/health` | Só o admin. A cada 5 s em Diagnóstico e Câmeras (12/min); nas outras telas, o sino a lê a cada 30 s. |
| 5 | `GET /api/deteccoes` | A cada 20 s na tela de Detecções, para as novas, mais uma página a cada rolagem. |
| 6 | `GET /api/rec/timeline` | De 10 a 60 s em Gravações, só no dia de hoje, conforme o tamanho do segmento. |
| 7 | `GET /api/rec/events` | Junto da timeline, só nas câmeras com o detector de objetos ligado. |
| 8 | `GET /api/session` | A cada 60 s no Ao vivo, e uma vez ao abrir a tela. |
| 9 | `GET /api/version` | No Ao vivo, enquanto a rede está caída: de 0,5 a 5 s entre tentativas. Com a rede boa, só ao abrir. |
| 10 | `GET /api/live/ws` | Uma conexão longa por câmera no Ao vivo. Poucas requisições, mas religa de 0,5 a 5 s quando cai. |

Fora do top 10: `GET /api/rec/init` (um por câmera ao abrir a reprodução),
`GET /api/avatar`, os arquivos da tela (uma rajada ao carregar, e o
`index.html` a cada 30 min, pelo aviso de versão nova) e as rotas de admin.
