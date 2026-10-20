# TODO - áreas na imagem, filtro por área e alertas de detecção

Planejado em 20/10/2026, sobre a `a9a87f2` (v0.11.0). **Status: em andamento.
Etapa 0 feita (`2de9b5a`).**

**Próximo passo:** a [Etapa 1](#etapa-1---filtrar-detecções-por-uma-área-desenhada-na-hora)
(filtrar Detecções por uma área desenhada na hora), com o mock já decidido:
o código, começando pelo `internal/area`, na branch `areas-e-alertas`.

São quatro pedidos, e um se apoia no outro:

1. **Áreas na imagem**, várias por câmera, desenhadas no cadastro da câmera. A
   detecção continua sendo gravada onde quer que aconteça; a área decide só se
   o alerta sai.
2. **Filtrar Detecções por área**: pelas áreas cadastradas e por uma área
   desenhada na hora, em cima de um quadro.
3. **Alerta de detecção** por e-mail, atrás de um contrato que aceite outros
   meios depois (Telegram, WhatsApp e afins). Antes do SMTP, o alerta aparece
   no log e na tela.
4. **Horários do alerta** por câmera: em que horas de cada dia da semana, de
   domingo a sábado.

Tudo com a interface pensada para o celular e para o desktop.

## Checklist

- [x] [Etapa 0 - toda caixa que virou marca fica gravada](#etapa-0---toda-caixa-que-virou-marca-fica-gravada)
- [ ] [Etapa 1 - filtrar Detecções por uma área desenhada na hora](#etapa-1---filtrar-detecções-por-uma-área-desenhada-na-hora)
- [ ] [Etapa 2 - áreas cadastradas na câmera](#etapa-2---áreas-cadastradas-na-câmera)
- [ ] [Etapa 3 - o alerta: contrato, regra e registro](#etapa-3---o-alerta-contrato-regra-e-registro)
- [ ] [Etapa 4 - o e-mail](#etapa-4---o-e-mail)
- [ ] [Etapa 5 - os horários do alerta](#etapa-5---os-horários-do-alerta)

## Decisões do mantenedor

Tomadas em 20/10/2026:

1. **A detecção continua sendo gravada em qualquer lugar da imagem.** A área
   decide o alerta e serve de filtro. Ela não muda o que o detector de objetos
   olha, nem o que vira marca, nem os números medidos do
   `internal/detect/parametros.go`.
2. **Várias áreas por câmera**, cadastradas na tela da câmera.
3. **"Dentro da área" tem duas regras candidatas, e quem escolhe é o uso.** A
   Etapa 1 sai com as duas, e a padrão é escolhida olhando detecções reais (ver
   [As duas regras](#as-duas-regras)).
4. **Toda caixa que virou marca passa a ser gravada**, e não só a de maior
   confiança de cada família. Vale daqui para frente; o que já está gravado
   fica como está.
5. **Destinatários do e-mail:** na primeira versão, uma lista no
   `dwnvr.yaml`, **opcional**. Preenchida, ela recebe o alerta de toda câmera
   com alerta ligado. Depois, cada pessoa que entra no dwnvr escolhe de quais
   câmeras quer receber (ver [Depois](#depois)).
6. **Antes do SMTP, o alerta vai para o log e para um card Alertas no
   Diagnóstico.** Os Avisos continuam sendo o que está errado no sistema: só
   ganham uma linha quando um meio de envio falha.
7. **Um contrato para os meios de envio**, para Telegram, WhatsApp e outros
   entrarem depois sem mexer no resto.
8. **Horários por câmera**, por dia da semana.
9. **Tudo numa branch à parte, a `areas-e-alertas`**, que faz o papel de
   `main` da feature. Ir para a `main` fica em aberto: a feature pode ir
   inteira, em parte, ou não ir (ver [Como entregar](#como-entregar)).

## O que o código já diz

Conferido na `a9a87f2`. É o que muda a conta das etapas:

1. **A marca de objeto é de chegada, e não de presença.** O rastreio
   (`internal/detect/marcador.go`) só marca caixa em lugar livre. Para o
   alerta, isso quer dizer:
   - um alerta por chegada, e não um por olhada: o carro parado na garagem
     avisa uma vez;
   - quem anda pela cena muda de lugar a cada olhada e marca de novo em cada
     uma: sem um freio, uma pessoa atravessando o quintal viraria vários
     e-mails (ver o silêncio, na Etapa 3);
   - a memória do rastreio não sobrevive a um reinício: depois dele, o objeto
     parado aparece uma vez como se tivesse acabado de chegar. Na timeline já é
     assim.
2. **A marca guarda uma caixa por família**, a de maior confiança
   (`Recorder.olhou`, em `internal/recorder/recorder.go`). Com duas pessoas no
   quadro, a caixa da outra se perde. É o que a Etapa 0 muda.
3. **A caixa é fração do quadro**, de 0 a 1. O quadro da detecção
   (`quadros/{dia}/{instanteMs}.jpg`, 480 px de largura) e o keyframe do
   segmento (`/api/rec/thumb`) têm o enquadramento do stream. Uma área guardada
   em fração vale sobre os dois, em qualquer resolução, e se compara com a caixa
   sem conversão.
4. **A marca chega atrasada.** Ela leva o instante do onset, mas só é gravada
   depois da olhada: antes vêm a janela do pico (3 s), a espera na fila e a
   olhada em si (3,6 a 6,5 s num Orange Pi Zero 3). Na reencenação que fixou a
   fila, o atraso foi de 16 s no p90 e de 30 s no p99. O alerta herda esse
   atraso.
5. **A detecção não marca toda chegada.** No nível 4, o mecanismo dispara em
   41,5% das chegadas, e cerca de um terço de todas elas chega à timeline com a
   família certa. O alerta avisa do que a detecção marcou e herda isso: **não é
   alarme**, e a tela e a doc precisam dizer.
6. **Detecções pagina por cursor, no servidor.** As marcas de objeto de cada
   dia ficam na memória do store (0,85 MB com 7.987 detecções, medido), e o
   calendário lê todas as câmeras de todos os dias (1,2 s com o cache de disco
   frio, ~5 ms depois). O intervalo de horário é filtrado na tela porque corta
   só as pontas da lista. Uma área corta no meio, e por isso o filtro dela vai
   ao servidor: na tela, as páginas viriam quase vazias, e a rolagem pediria
   dezenas delas.
7. **Mudar a câmera não reconecta, salvo áudio, segmento e stall**
   (`recordingParamsChanged`, em `internal/recorder/manager.go`). Área e alerta
   entram pelo `trocaCamera`, com a gravação no ar.
8. **O `POST /api/cameras` lê até 16 KB.** Oito áreas de 20 pontos ocupam
   ~3 KB.
9. **O viewer recebe as câmeras resolvidas** no `GET /api/cameras`: o que for
   campo da câmera chega também à tela do viewer.
10. **Os Avisos são o estado do sistema** (`web/src/lib/avisos.ts`): o que
    está errado agora, derivado do `/api/health`. O sino do header os conta e
    fica vermelho com um só. Uma pessoa detectada não é defeito.
11. **O `docs/deteccao.md` diz que a detecção "não avisa ninguém".** Muda com
    a Etapa 3.
12. **A área não descarta nada.** Ela não muda o que é olhado nem o que vira
    marca: é uma escolha de uso ("onde me importa"), e não um ajuste técnico da
    detecção que caia no colo de quem usa.
13. **O De e o Até já existem**: o `IntervaloDeHorario` e o `CampoHora` de
    Detecções, com a regra de que o "até" menor que o "de" termina no dia
    seguinte. Os horários do alerta reaproveitam os dois, e a regra.

## A sequência

```mermaid
flowchart LR
    e0["Etapa 0<br>todas as caixas"] --> e1["Etapa 1<br>área desenhada<br>em Detecções"]
    e1 --> e2["Etapa 2<br>áreas da câmera"]
    e2 --> e3["Etapa 3<br>o alerta"]
    e3 --> e4["Etapa 4<br>o e-mail"]
    e4 --> e5["Etapa 5<br>os horários"]
    e5 -.-> depois["depois<br>cada pessoa escolhe<br>outros meios"]
```

**Começar pelo filtro de uma área desenhada na hora, a ideia do mantenedor, é
o melhor começo**, por quatro motivos:

1. É onde nascem as duas peças que todo o resto usa: o editor de área, a parte
   mais difícil de acertar no celular, e o teste "o objeto está na área?".
2. Não guarda nada: nem `cameras.json`, nem `dwnvr.yaml`, nem mudança
   incompatível. É a fatia mais barata.
3. É a bancada da regra. Com as duas regras sobre detecções reais, a padrão é
   escolhida antes de ela decidir quem recebe um e-mail às 3 da manhã.
4. Prepara o alerta. Com as áreas da câmera (Etapa 2), filtrar Detecções por
   elas mostra exatamente o que teria alertado.

O cuidado: o mock do editor já considera os dois lugares onde ele vai morar, a
folha da detecção e o cadastro da câmera, para a Etapa 2 não refazer o
componente.

**A Etapa 0 vem antes** porque é pequena e porque o histórico com todas as
caixas só começa no deploy dela: quanto antes, mais dado certo para testar o
filtro da Etapa 1.

**O e-mail vem logo depois do motor do alerta**, e os horários depois dele: o
e-mail é o que faz o alerta chegar a alguém, e os horários refinam quando.

### Como entregar

**Tudo na branch `areas-e-alertas`** (decisão do mantenedor, 20/10/2026),
criada da `origin/main` na `a9a87f2`. Ela faz para esta feature o papel da
`main`: recebe só commits fechados, um por mudança, cada um com o `dist` e a
doc do que mudou. Ir para a `main` fica em aberto, porque ainda não se sabe
como a feature vai se desenrolar: ela pode ir inteira, em parte, ou não ir. Se
for, vai commit a commit, por rebase, sem squash e sem merge commit.

O plano recomendava a `main` aos poucos, um PR por etapa. Com a branch longa,
três cuidados seguram o custo:

- **Rebasear sobre a `main` no começo de cada etapa**, quando ela andou.
  `Deteccoes.svelte`, `Cameras.svelte`, `api.ts` e o `internal/api/dist/` mudam
  em quase todo PR da `main`, e o conflito pequeno de cada vez sai mais barato
  que o grande no fim. O `dist` é refeito em cada commit rebaseado, nunca
  costurado à mão. Depois de a branch ir ao GitHub, rebasear pede
  `--force-with-lease`.
- **Testar no uso por deploy manual.** A instalação de teste se atualiza pela
  `main`, e a branch só chega lá à mão.
- **O uso de uma etapa pode mudar uma anterior.** A regra da área, escolhida no
  uso da Etapa 1, muda a 2 e a 3. Numa branch que faz papel de `main`, isso é
  um commit novo de ajuste ou o amend do commit da etapa, como o mantenedor
  preferir na hora.

### O fluxo de cada etapa

1. Na branch `areas-e-alertas`, rebaseada sobre a `main` se ela andou.
2. Com decisão de tela, um mock HTML local com as variantes lado a lado, nos
   tokens e classes do `web/src/app.css`, em 390 px e 1280 px. Imagem real de
   câmera só no mock local, nunca no repositório.
3. Implementar, com `make test` e `prek run --all-files` verdes.
4. O mantenedor testa: no celular, build + `vite preview`; quando o servidor
   muda, um dwnvr local com cópia dos eventos da instalação de teste, ou a
   branch na própria instalação de teste, por deploy manual.
5. O diff e a mensagem; o commit só com o ok do mantenedor. O push da branch
   fica com o mantenedor.
6. A etapa marcada neste arquivo, no mesmo commit.

## Vocabulário

| Termo | O que é |
| --- | --- |
| **área** | um polígono sobre a imagem de uma câmera, em fração do quadro. Várias por câmera, cada uma com nome |
| **regra da área** | quando um objeto conta como dentro: `encosta` ou `pisa` |
| **alerta** | o aviso de que uma detecção passou na regra de alerta da câmera |
| **meio** | por onde o alerta sai: e-mail, depois outros |
| **registro** | a linha do alerta no log e no card Alertas. Todo alerta tem, com meio configurado ou não |
| **silêncio** | o tempo, depois de um alerta, em que a mesma câmera e família não alertam de novo |

## As duas regras

| Regra | Conta como dentro quando | A favor | Contra |
| --- | --- | --- | --- |
| `encosta` | a caixa e a área têm algum ponto em comum | é o que se espera sem pensar; pega o objeto pela metade na borda | em perspectiva, quem está perto da câmera cobre a área que fica atrás: a pessoa no quintal encosta no portão do fundo |
| `pisa` | o meio da borda de baixo da caixa, onde ficam os pés e as rodas, está dentro da área | a área se desenha no chão, e quem está na frente dela não conta | com os pés escondidos por um muro, quem conta é a borda do muro; e a tela precisa explicar que a área é o chão |

As duas são um teste geométrico proporcional aos pontos da área, sem alocar.

## Etapa 0 - toda caixa que virou marca fica gravada

**O que muda para quem usa:** duas pessoas que chegam juntas aparecem com as
duas caixas na miniatura e na folha de Detecções e sobre o vídeo de Gravações.
O filtro por área acha a detecção pela pessoa que estiver na área, seja ela a
de maior confiança ou não. Na timeline nada muda: continua uma chegada por
família.

**Como:**

- `Recorder.olhou` grava uma linha por objeto que virou marca, cada uma com a
  sua classe, confiança e caixa, todas no instante do onset e apontando para o
  mesmo quadro. Hoje é uma por família, com a de maior confiança (o mapa
  `melhor`).
- A linha não muda de forma: só passa a haver mais de uma da mesma família no
  mesmo instante.
- `Timeline.svelte` (`grupos`) junta por família dentro do instante. Sem isso,
  duas pessoas virariam dois ícones de pessoa, e o tique se dividiria em duas
  faixas da mesma cor.
- `/api/deteccoes` (`agrupa`) já junta as marcas do instante numa detecção, e
  passa a trazer todos os objetos. A miniatura, a folha e as caixas sobre o
  vídeo já desenham o que vier.
- O calendário (`contaDeteccoes`) conta instantes, e o funil conta olhadas:
  nenhum dos dois muda.
- A cauda de Gravações (`caudaDeEventos`) descartava o que já tinha por
  instante e família. Passa a comparar a caixa: a rodada que caísse entre as
  duas linhas da mesma família perderia a segunda até recarregar a página.

**Descartado:** aninhar as outras caixas na linha da família. Deixaria os
leitores de hoje intactos, mas o filtro, o alerta e a API teriam de olhar em
dois lugares para sempre.

**Medir depois do deploy:** quantos instantes passam a ter duas marcas da
mesma família, por câmera e por dia. É o tamanho do ganho, e o do crescimento
da memória das marcas.

**Não é mudança incompatível:** caminho, `DayLayout` e formato da linha são os
mesmos. Voltar para a versão anterior não quebra: ela lê as linhas a mais como
marcas extras no mesmo instante, e a timeline mostra dois ícones.

**Repercussões:** `docs/arquitetura.md` (§Armazenamento, a linha de objeto),
`docs/deteccao.md` (o passo 7, "grava no máximo uma marca por família"),
`docs/api.md` (`/api/rec/events`, e "Pessoa e carro no mesmo quadro são duas
marcas" em §Detecções).

- [x] uma linha por objeto no `Recorder.olhou`, com teste
- [x] a timeline junta por família no instante
- [x] docs
- [x] print de uma detecção com duas caixas da mesma família
- [x] ~~a medição do primeiro dia no ar~~: dispensada em 20/10/2026. O
  mantenedor viu no ar, no primeiro dia, duas caixas de pessoa no mesmo quadro,
  e isso basta para fechar a etapa; a contagem por câmera e por dia não mudaria
  nada do que vem depois.

## Etapa 1 - filtrar Detecções por uma área desenhada na hora

**O que muda para quem usa:** em Detecções, desenhar uma área em cima do
quadro de uma câmera e ver só as detecções com objeto dentro dela. É a
pergunta "quem passou pelo portão nesta semana?".

**Como fica**, decidido no mock em 20/10/2026:

- **Duas entradas.** Na folha de uma detecção, o botão "filtrar por área" fica
  ao lado de "ocultar câmera", desenha sobre aquele quadro, e a câmera já é a
  dela. Na barra, um botão de área pede a câmera num popover e abre o quadro
  da detecção mais recente dela, ou o keyframe do último segmento quando ela
  não tem detecção. No celular o botão fica na 1ª linha, junto do intervalo;
  se apertar em alguma largura, muda de lugar. No desktop ele vem logo depois
  do intervalo, com o nome escrito.
- **O editor é o polígono livre** (a variante A do mock): cada toque põe um
  vértice, e tocar no primeiro fecha a área. Arrastar um vértice ajusta, e há
  desfazer e limpar. No celular, em retrato e deitado, na tela cheia; deitado,
  os controles vão para uma coluna à direita. No desktop, um modal. Foram
  descartados o retângulo que vira polígono (B) e o pincel numa grade (C), que
  trocaria o polígono por uma grade nas Etapas 2 e 3.
- **Aplicado, um chip na barra**, como o do intervalo de horário: a câmera e
  "área". Tocar reabre o editor, e o ✕ limpa. No celular, com o intervalo
  também ligado, os dois chips dividem a 3ª linha; sozinho, ele a ocupa
  inteira. No desktop, os dois chips na mesma linha, como o intervalo hoje.
- **A regra é escolha do usuário**, num seletor do editor: `encosta` (a caixa
  toca a área) ou `pisa` (o pé, meio da base da caixa, está dentro), com
  `pisa` de padrão. O nome `pisa` fica por ora e pode mudar na tela depois. Com casos
  reais de 16 a 20/10/2026, de 20% a 43% do que a `encosta` mostra é gente que
  só aparece por cima da área, perto da câmera, ou um falso positivo parado
  na borda; o preço da `pisa` é desenhar a área cobrindo todo o chão onde o pé
  pode estar.
- **A câmera do filtro passa a ser a da área:** as outras saem da lista
  enquanto o filtro estiver ligado, e o botão de câmeras fica preso nela.
- **O contorno da área**, fino e tracejado, nas miniaturas; na folha, sempre.

**Servidor:**

- pacote novo `internal/area`, com o tipo `Area` (de 3 a `MaxPontosDaArea`
  pontos, em fração do quadro), o contrato `RegraDaArea` (o que em Java seria
  uma interface) com as duas implementações, o mapa nome -> construtor e o
  `parametros.go`;
- `GET /api/deteccoes` e `GET /api/deteccoes/dias` aceitam
  `area=<cam>:<x>,<y>,<x>,<y>,...`, em milésimos do quadro (inteiros de 0 a
  1000), e `regra=`;
- o filtro tira da detecção o objeto que não está na área, como o de família
  já faz, e a detecção que fica sem objeto sai da página;
- marca sem caixa, de antes de a caixa existir, fica de fora com o filtro
  ligado;
- o calendário segue a área, como segue câmeras e famílias. O intervalo de
  horário não segue, por decisão de 19/10/2026.

**Tela:**

- o editor é um componente próprio, num chunk à parte (`import()`), que a
  Etapa 2 reaproveita: quem não desenha área não baixa nada;
- a URL leva `area=` e `regra=`, e o link colado em outra aba mostra a mesma
  lista;
- o viewer também usa: o filtro não grava nada.

**Medir:** a página e o `/api/deteccoes/dias` com área, com o cache de disco
frio e quente, no hardware de referência. O teste por marca é barato; o que
importa é o calendário, que passa por todas as marcas de todos os dias.

**Como conferir:** área côncava (em L); caixa que só encosta na borda; área do
tamanho da imagem, que tem de dar o mesmo que sem filtro; câmera 4:3, em que a
caixa e a área são fração da imagem, e não da miniatura 16:9; link colado em
outra aba; o dedo no celular, em retrato e deitado.

**A regra padrão é a `pisa`** (`RegraDaAreaPadrao`), e o seletor fica. Na
Etapa 2, a regra vira campo de cada área cadastrada.

**Repercussões:** `docs/api.md` (§Detecções), `web/src/lib/api.ts`,
§Estrutura do `web/README.md` (o editor), árvore do `README.md`
(`internal/area/`).

**Não é mudança incompatível:** os parâmetros são novos e opcionais.

- [x] mock do editor (A, B, C), nos dois contextos, celular e desktop
- [ ] `internal/area`, com teste das duas regras
- [ ] `area=` e `regra=` na API, com teste
- [ ] o editor, o chip e a URL
- [ ] a medição no hardware de referência
- [ ] teste do mantenedor no celular e no desktop
- [x] a regra padrão escolhida: `pisa`, e o seletor fica
- [ ] docs

## Etapa 2 - áreas cadastradas na câmera

**O que muda para quem usa:** no cadastro da câmera, o admin desenha e dá nome
às áreas dela, como "Portão" e "Garagem". Em Detecções, qualquer pessoa filtra
por elas sem desenhar de novo.

**O dado** (proposta), no `cameras.json`:

```json
"areas": [
  { "id": "k3f9a2", "nome": "Portão", "regra": "pisa", "pontos": [[0.12, 0.3], [0.55, 0.3], [0.55, 0.8], [0.12, 0.8]] }
]
```

- O `id` é curto e gerado pelo servidor quando a área chega sem ele. O link de
  Detecções e o alerta apontam para ele, e renomear a área não quebra nenhum
  dos dois.
- `ConfereCamera` confere: nome não vazio e sem repetir na câmera; até
  `MaxAreasPorCamera` áreas; de 3 a `MaxPontosDaArea` pontos; coordenadas de 0
  a 1. A API recusa com o motivo, e o boot tira a área inválida e avisa, como
  já faz com os outros campos.
- A `regra` é a da Etapa 1, escolhida no editor, e vale para o filtro e para o
  alerta daquela área. Ausente, é a `RegraDaAreaPadrao`.
- Muda com a gravação no ar (`trocaCamera`).

**Como fica** (a fechar no mock). O mock já desenha o formulário final, com as
seções do alerta e dos horários, para a tela não ser refeita a cada etapa:

- **onde as áreas ficam no cadastro**, em três variantes:
  - **A - seção no formulário:** a lista das áreas, cada uma com a miniatura e
    o nome, e "nova área" abrindo o editor em tela cheia;
  - **B - botão no card da câmera:** "áreas (2)", com uma folha própria, fora
    do formulário;
  - **C - formulário em abas:** gravação, detecção, áreas e alertas.
- **o quadro de fundo:** o keyframe do último segmento, decodificado no
  navegador em tamanho cheio, com um "atualizar o quadro". O `thumbs.ts`
  reduz a imagem para a tira de miniaturas, e assim não serve. A outra opção é
  o quadro da última detecção, de 480 px;
- **"salvar como área da câmera"**, a partir da área desenhada na Etapa 1, só
  para o admin: decidir no mock.

**Em Detecções:** o botão de área lista as áreas cadastradas, por câmera, mais
"desenhar uma área". Escolher várias soma: qualquer uma serve. A URL leva
`areas=<cam>/<id>`, e o servidor resolve pelo cadastro: editar a área muda o
filtro dali em diante, inclusive sobre o passado.

**O enquadramento:** se a câmera for movida, a área fica no lugar errado. A
doc diz para redesenhar, e o editor sempre mostra o quadro atual.

**Repercussões:** campo em `config.Camera`: `config.Resolve`,
`config.ConfereCamera`, `Cameras.svelte` e §Política por câmera do
`docs/configuracao.md`. E ainda `docs/api.md` e `web/src/lib/api.ts`.

**Não é mudança incompatível**, com uma ressalva para as notas da release:
voltar para uma versão anterior e salvar a câmera apaga as áreas dela, porque a
versão velha não conhece o campo.

- [ ] mock do cadastro (A, B, C), já com as seções de alerta e de horários
- [ ] o campo `areas`, com validação e teste
- [ ] o cadastro na tela Câmeras
- [ ] o filtro por área cadastrada em Detecções
- [ ] teste do mantenedor no celular e no desktop
- [ ] docs

## Etapa 3 - o alerta: contrato, regra e registro

**O que muda para quem usa:** no cadastro da câmera, "avisar quando
detectar", com as famílias e as áreas que contam. Nesta etapa o alerta vai para
o log e para o card Alertas do Diagnóstico; o e-mail é a Etapa 4.

**O dado** (proposta), no `cameras.json`:

```json
"alerta": { "ligado": true, "familias": ["pessoa"], "areas": ["k3f9a2"] }
```

- `areas` vazio é a imagem inteira. A tela diz isso por extenso, "em qualquer
  lugar da imagem" ou "só nas áreas marcadas", porque desmarcar todas as áreas
  não pode parecer "sem alerta".
- `familias` começa em `pessoa`, a de maior prioridade. Uma câmera voltada para
  a rua, com `veiculo`, daria até ~450 alertas por dia antes do silêncio: é
  quanto uma câmera assim marca de veículo na instalação de teste.
- Desligar guarda o resto, para religar sem refazer.
- A seção só aparece com o detector de objetos configurado e a marcação de
  movimento ligada na câmera: sem marca de objeto, não há o que avisar.

**Onde avalia:** no `Recorder.olhou`, logo depois do `Marcador`, com todas as
caixas que viraram marca e o quadro olhado. Vale o instante do onset, que é a
hora da cena, e não o da resposta.

**O alerta nunca segura a fila do detector de objetos.** O `olhou` roda na
goroutine da fila: esperar um servidor SMTP ali pararia as olhadas de todas as
câmeras. O alerta vai para um canal com buffer (`AlertasNaEspera`), e uma
goroutine de envio o entrega aos meios. Com o canal cheio, o alerta é
descartado e contado.

**O contrato**, num pacote novo `internal/alerta` (o que em Java seria uma
interface):

```go
// Notificador é um meio de avisar alguém: o e-mail agora; Telegram e WhatsApp
// depois. Não guarda estado entre envios: quem decide o que vira alerta, e
// quando, é o pacote alerta.
type Notificador interface {
	// Envia entrega um alerta, e só volta quando o meio aceitou ou recusou,
	// dentro do prazo do ctx.
	Envia(ctx context.Context, a Alerta) error

	Nome() string
}

// Notificadores é o mapa de construtores: um meio novo entra numa linha.
var Notificadores = map[string]func(c config.Alertas) (Notificador, error){
	"email": NovoEmail, // Etapa 4
}
```

- O `Alerta` leva a câmera (id e nome), o instante do onset e o do quadro, os
  objetos que passaram na regra (família, classe, confiança, caixa e as áreas
  em que caíram) e o JPEG do quadro olhado.
- Todo alerta vira uma linha `INFO` no log, com a câmera, as famílias e as
  áreas, e entra no card Alertas. Esse é o **registro**, que existe com meio
  configurado ou sem. É ele que se testa nesta etapa.

**O silêncio:** por câmera e família, um alerta abre um silêncio de
`SilencioDoAlertaMs`, em que a mesma câmera e família não alertam de novo. O que
cai nele é contado como silenciado. É por família, e não por câmera, porque
`pessoa` vale mais: o gato de um minuto atrás não pode calar a pessoa de agora.
**O número sai de uma simulação** sobre o histórico real de marcas, antes de
ser fixado: quantos alertas por dia cada câmera daria com 1, 5 e 15 min de
silêncio, com as áreas e as famílias que seriam usadas de verdade.

**O objeto parado depois de um reinício** vira chegada na primeira olhada
(item 1 de [O que o código já diz](#o-que-o-código-já-diz)). Com a família
padrão, `pessoa`, ele só incomoda quando o modelo confunde algo parado com
gente, dentro da área. Com `veiculo` na garagem, cada reinício avisaria do
carro que está lá. As saídas:

- **a) aceitar e medir no uso.** Recomendo para começar.
- **b) uma olhada de aquecimento por câmera ao subir**, que ensina o rastreio
  sem marcar nem alertar. Conserta a timeline também; custa uma olhada por
  câmera a cada reinício e mexe no funil, porque é olhada sem onset, como a de
  [`TODO_olhada-forcada-sem-onset.md`](TODO_olhada-forcada-sem-onset.md).
- **c) gravar o rastreio em disco**, em segundo plano, e lê-lo ao subir.

Não alertar "a primeira olhada depois de subir" não serve: ela pode acontecer
horas depois, e ser justamente a chegada que importa.

**No Diagnóstico**, só para o admin:

- **o card Alertas**, que só existe com alguma câmera com alerta ligado: os
  últimos `AlertasNaTela` alertas, de memória (somem num reinício), cada um com
  a hora, a câmera, o que foi visto, as áreas e o resultado em cada meio; e os
  números desde que o dwnvr subiu: enviados, silenciados, descartados e
  falhas;
- **um Aviso `bad` só quando um meio falha**, com o "desde quando" dos outros
  avisos, como "Alertas por e-mail falhando desde 13:58 (12min): o erro. As
  detecções seguem gravadas, mas ninguém está sendo avisado." Vem do
  `/api/health`, como os outros.

**No cadastro da câmera**, a seção Alertas desenhada no mock da Etapa 2:
ligar, famílias, onde (a imagem inteira ou as áreas marcadas) e para onde vai,
que até a Etapa 4 é "só o registro". E uma linha que diga o limite: o alerta
avisa do que a detecção marcar, e ela não pega toda chegada (item 5 de
[O que o código já diz](#o-que-o-código-já-diz)).

**Custo zero desligado:** sem câmera com alerta ligado, nada nasce: nem canal,
nem goroutine, nem card, nem os bytes da seção, que ficam num chunk à parte.

**Repercussões:** `docs/deteccao.md` (sai o "não avisa ninguém", e entram uma
seção sobre o alerta e os termos novos no vocabulário), `docs/api.md`
(`/api/health` e o campo da câmera), §Política por câmera do
`docs/configuracao.md`, §Detecção de movimento e de objetos do `README.md`,
`web/src/lib/api.ts`, `web/README.md` e a árvore do `README.md`
(`internal/alerta/`).

**Não é mudança incompatível:** o campo é novo e opcional, e o alerta vem
desligado.

- [ ] a simulação do silêncio sobre o histórico real
- [ ] `internal/alerta`: o contrato, a regra, o silêncio e o envio, com teste
- [ ] a avaliação no `Recorder.olhou`, sem segurar a fila
- [ ] o campo `alerta`, com validação
- [ ] a seção Alertas no cadastro
- [ ] o card Alertas e o Aviso de meio falhando
- [ ] na instalação de teste, por deploy manual, com detecções reais
- [ ] docs

## Etapa 4 - o e-mail

**O que muda para quem usa:** o alerta chega por e-mail, com o quadro da
detecção.

**A configuração** fica no `dwnvr.yaml`, numa seção nova e opcional, com as
chaves no estilo do arquivo (`username` e `password`, como nos blocos `server`
e `go2rtc`):

| Campo | O que é |
| --- | --- |
| servidor e porta | o SMTP do provedor |
| segurança | STARTTLS (porta 587), TLS direto (465) ou nenhuma, só para rede local |
| usuário e senha | a conta que envia |
| remetente | o `From`, como `dwnvr <alertas@exemplo.com>` |
| destinatários | **opcional**: recebem o alerta de toda câmera com alerta ligado |

Sem destinatário, o e-mail fica configurado e ninguém recebe; o card Alertas
diz isso. A senha fica no servidor: não vai à tela nem ao log.

**A mensagem:**

- o assunto leva a família de maior prioridade, a câmera e a hora do onset,
  como "Pessoa em Garagem às 14:32";
- o corpo leva o dia, os objetos e as áreas em que caíram;
- o quadro olhado vai anexo em JPEG, com as caixas e a área desenhadas.
  Desenhar é Go puro (`image/jpeg` e `image/draw`) sobre um quadro de 480 px:
  medir no hardware de referência, em que a ordem esperada é de dezenas de ms;
- sem link para abrir a detecção: o servidor não sabe o endereço pelo qual é
  acessado, e o link de convite usa o do navegador do admin. Fica para quando
  houver um campo para isso.

**Implementação:** biblioteca padrão do Go, sem C e sem dependência nova:
`net/smtp`; `crypto/tls`, porque a porta 465 pede o `tls.Dial` antes do
`smtp.NewClient`; `mime/multipart` para o anexo; e `mime.QEncoding` para o
assunto com acento.

**Limites**, no `parametros.go`: `PrazoDoEnvioMs`, e um teto de e-mails por
hora para a instalação inteira. O teto protege a conta, porque o provedor
bloqueia quem dispara demais. O que passa dele é contado e vira Aviso.

**Testar:**

- um botão "enviar e-mail de teste" no card Alertas, só para o admin
  (`POST /api/alertas/teste`), que mostra o erro do servidor SMTP como ele
  veio;
- para desenvolver sem mandar e-mail de verdade, o Mailpit num container, com
  o SMTP na porta 1025 e a caixa de entrada na 8025;
- provedores como o Gmail pedem uma senha de app, com a verificação em duas
  etapas ligada: a doc dá o exemplo.

**Repercussões:** campo em `internal/config/config.go`: `dwnvr.example.yaml`,
com comentário, uma seção nova no `docs/configuracao.md` e a §Configuração do
`README.md`. A rota nova: `docs/api.md` e `web/src/lib/api.ts`.

**Não é mudança incompatível:** a seção é nova e opcional.

- [ ] a seção no `dwnvr.yaml`, com validação
- [ ] o meio `email`, com teste contra o Mailpit
- [ ] o quadro com as caixas e a área desenhadas, medido
- [ ] o teto por hora e o Aviso
- [ ] o botão de teste
- [ ] docs, com o exemplo do Gmail

## Etapa 5 - os horários do alerta

**O que muda para quem usa:** por câmera, em que dias e horas o alerta sai.
Sem horário, ele sai sempre.

**O dado** (proposta), dentro do `alerta`:

```json
"horarios": [
  { "dias": [1, 2, 3, 4, 5], "de": "22:00", "ate": "06:00" },
  { "dias": [0, 6], "de": "00:00", "ate": "00:00" }
]
```

- `dias` vai de 0 (domingo) a 6 (sábado), como o Go e o JavaScript contam.
- O "até" menor que o "de" termina no dia seguinte, e o dia da faixa é o do
  início: sexta das 22:00 às 06:00 cobre a madrugada de sábado. É a mesma regra
  do intervalo de Detecções.
- O "de" igual ao "até" é o dia inteiro.
- Vale o instante do onset, no fuso do servidor, o mesmo que vira o dia das
  gravações.

**Como fica** (a fechar no mock), em três variantes:

- **A - a grade da semana:** 7 dias x 24 horas, pintada de hora em hora com o
  dedo ou o mouse. No celular, os dias viram colunas e as horas, linhas.
- **B - um dia por linha:** cada dia com as faixas dele, "+ faixa" e "copiar
  para os outros dias".
- **C - faixas com os dias em chips:** cada faixa tem os dias (D S T Q Q S S)
  e o De e o Até do `CampoHora`, com a semana desenhada embaixo como prévia e
  atalhos (sempre, toda noite, fim de semana).

Recomendo a C: reaproveita o De e o Até que já existem, vai ao minuto, cabe no
celular sem gesto de pintar, e a prévia mostra a semana inteira de uma vez. A
A é a mais visual, e a mais difícil no dedo.

**Repercussões:** `config.ConfereCamera` (dias e horas válidos), §Política
por câmera do `docs/configuracao.md`, `docs/deteccao.md` e
`web/src/lib/api.ts`.

**Não é mudança incompatível:** o campo é novo e opcional.

- [ ] mock (A, B, C), celular e desktop
- [ ] o campo `horarios`, com validação e teste da virada do dia
- [ ] a seção no cadastro
- [ ] teste do mantenedor no celular e no desktop
- [ ] docs

## Depois

Sem data. Cada item vira plano próprio quando chegar a vez.

- **Cada pessoa escolhe as câmeras** (decisão do mantenedor): o e-mail na
  Minha conta e as câmeras de que ela quer alerta, somados à lista global sem
  repetir endereço. Pede um campo de e-mail no `usuarios.json` e, já com o SMTP
  funcionando, um e-mail de confirmação, para não guardar endereço digitado
  errado.
- **Outros meios**, cada um uma linha no `Notificadores`:
  - **Telegram:** um bot e o chat de destino. O `sendPhoto` manda o quadro, por
    HTTPS;
  - **ntfy:** um `POST` com o quadro anexo. Tem app no celular e pode rodar na
    própria casa;
  - **webhook genérico:** um `POST` com o JSON do alerta, para quem integra com
    outra coisa;
  - **WhatsApp:** não tem caminho simples. A API oficial exige conta business e
    mensagem pré-aprovada, e os caminhos não oficiais arriscam o bloqueio do
    número.
- **Regra por área:** famílias e horários diferentes em cada área, como
  "pessoa no portão a qualquer hora, veículo na garagem só à noite".
- **Marcar em Detecções o que alertou**, e filtrar por isso. Pede gravar o
  alerta enviado, e não só lembrar dele.
- **Um link para abrir a detecção** no alerta: pede um campo com o endereço
  público do dwnvr.
- **Notificação do navegador (push):** exige service worker, que o dwnvr evita
  de propósito (ver "Instalável como app, sem service worker" no
  `web/README.md`).

## Os limites, para a doc do alerta

- **Não é alarme.** O alerta avisa do que a detecção marcou, e ela não marca
  toda chegada (item 5 de [O que o código já diz](#o-que-o-código-já-diz)).
  Subir a sensibilidade pega mais, e custa mais CPU.
- **Chega atrasado:** de segundos a meio minuto depois da cena, mais o tempo do
  próprio e-mail.
- **A fila descarta:** numa câmera agitada, o pedaço que não cabe na fila vira
  só movimento, e não alerta.
- **Falso positivo vira alerta falso:** o que o modelo confunde com gente,
  num lugar novo, avisa. A área, as famílias e o horário são o freio.
- **Com o detector de objetos fora do ar, nenhum alerta sai.** O Aviso que já
  existe diz desde quando.
- **Câmera movida, área errada:** é preciso redesenhar.
