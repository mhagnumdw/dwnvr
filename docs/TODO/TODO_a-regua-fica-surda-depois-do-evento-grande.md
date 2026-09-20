# A régua do score fica surda depois de um evento grande

**Em 3 linhas:** a cauda de um evento (o carro manobrando, a porta, a sombra)
entra na média e no desvio do score, e o desvio é o denominador do z - então no
minuto seguinte a um evento grande a câmera fica surda justamente quando a
pessoa desce do carro. Congelar o aprendizado durante o evento inteiro conserta
esse caso e **piora o agregado**, porque numa câmera movimentada a régua nunca
descongela. O caminho que sobra é congelar **com teto**, e isso ainda não foi
medido.

A medição completa está em
[`anexo_congelamento-da-regua-a-medicao.md`](anexo_congelamento-da-regua-a-medicao.md).

## O sintoma, medido

`cam_lateral1`, 20/09/2026, 10:45. Um carro entra na garagem, manobra por 18 s,
e às 10:45:49 uma pessoa desce dele e atravessa o quadro. A timeline marcou o
carro e **não marcou a pessoa**.

| instante | tamanho do quadro | média da régua | desvio (log) | z | destaque |
|---|---|---|---|---|---|
| 10:44:52, antes do carro | 2.092 B | 525 B | 0,414 | **3,20** | **3,09 → onset** |
| 10:45:51, a pessoa descendo | **3.592 B** | 942 B | 0,832 | 1,62 | **-0,87 → nada** |

A pessoa produziu quadros **71% maiores** que os que dispararam o onset um
minuto antes, e o gatilho os considerou normais. Não é o modelo: forçando uma
olhada nesse instante, o `dwnvr-detect` devolve `person=0,841`.

`CongelaAcima` (3,0) só bloqueia o aprendizado quadro a quadro, enquanto
`z >= 3`. A **cauda** do evento, com z entre 0 e 3, é aprendida - e é ela que
dobra a média e dobra o desvio.

## O que já foi tentado, e por que não serve

`kleinberg-c`: o congelamento com histerese própria - abre em `CongelaAcima` e
só fecha depois de N quadros seguidos abaixo de `SaiDoEventoZ`.

Medido sobre **379,9 horas**, 18 câmera-dias, as 9 câmeras, com as duas funções
calibradas para o **mesmo custo** (100 olhadas/h por câmera) e o `dwnvr-detect`
de produção como juiz:

| família | hoje | candidata | perdas | ganhos | **saldo** |
|---|---|---|---|---|---|
| **pessoa** | 1.018 | 900 | 340 | 222 | **-118** |
| veiculo | 689 | 642 | 122 | 75 | -47 |
| animal | 192 | 154 | 85 | 47 | -38 |
| **TOTAL** | **1.899** | **1.696** | 547 | 344 | **-203** |

Ela **resolve o episódio de 10:45** (só ela dispara ali) e entrega 203 marcas a
menos no total, sendo 118 de `pessoa`, que é a família de maior prioridade.

A causa do estrago é a mesma do ganho: ela para de aprender. Onde o
congelamento fica em 15-20% do tempo, ela ganha; onde prende, desaba.

| câmera | saldo | tempo congelado |
|---|---|---|
| cam_cozinha | **-198** | 52-67% |
| cam_lateral1 | -17 | 45-64% |
| cam_jardim | **+23** | 18-20% |
| cam_fundo | +8 | 16% |

A `cam_cozinha` sozinha responde por 198 das 203 marcas perdidas.

## Os próximos passos que eu sugiro

**1. Congelar com TETO (o candidato mais promissor).** No máximo N quadros
congelados seguidos; passando disso, a régua volta a aprender à força, mesmo
com z alto. Isso mantém o ganho da cauda curta - que os +344 mostram ser real -
sem a régua presa que quebra a `cam_cozinha`. Varrer N em torno de 100 a 600
quadros (10 s a 1 min a 10 fps) e medir com a bancada.

**2. Medir o anoitecer, antes de adotar qualquer variante de congelamento.**
Nenhuma das medições cobriu a transição para o infravermelho, quando o bitrate
da câmera muda de patamar. Régua congelada é régua que não acompanha essa
mudança - e é o pior momento para ficar cego. O corpus atual (18 e 19/09) tem
as noites; falta separar o recorte.

**3. Se o teto também reprovar, atacar o problema por outro lado.** A surdez
também se resolveria com uma segunda olhada forçada alguns segundos depois de
um onset grande, sem mexer na régua - o que custa orçamento de detecção em vez
de risco estatístico. Ver
[`TODO_olhada-forcada-sem-onset.md`](TODO_olhada-forcada-sem-onset.md), que
ataca um caso vizinho.

**O que NÃO vale a pena:** recalibrar o limiar de produção. Ele rende 91,9
onsets/h contra os 100 nominais no nível 5 (e 44,5 contra 50 no nível 4) - 8% a
11% de folga que não chega perto de explicar o caso.

## Como retomar

A bancada que produziu estes números (`detect-test-v5/`) **não está no
repositório** - é descartável e os dados dela são dezenas de GB. Ela ficou em
`~/dwnvr-lab-v5/`, com um `COMO-RODAR.md` explicando que precisa voltar para
dentro do módulo Go para compilar. Três coisas nela são retomáveis: as séries
de tamanho de quadro já extraídas, os pedaços já cortados e o cache do
detector, chaveado pelo conteúdo do pedaço. Um candidato novo roda com um
comando:

```sh
./detect-test-v5/roda.sh kleinberg-t:2.0:10:300 5
```

O `kleinberg-c` medido aqui **foi removido de `internal/detect`**: ele reprovou,
e o comentário do mapa `FuncoesDeScore` diz que só as que venceram a medição
ficam lá. O código dele, a fixture com os quadros reais deste episódio e o
construtor exportado `detect.NovoEstatistico` de que a calibração precisa estão
no histórico do git, no commit desta medição.
