# Anexo: a medição do congelamento da régua

Os números completos por trás de
[`TODO_a-regua-fica-surda-depois-do-evento-grande.md`](TODO_a-regua-fica-surda-depois-do-evento-grande.md).
A bancada que os produziu (`detect-test-v5/`) não é versionada: ela é
descartável e depende de dezenas de GB de gravação. O que fica é este anexo.


Medição de uma mudança candidata no score do gatilho de movimento, feita em
20/09/2026 sobre as gravações do pitoco.

## O problema

O score compara cada quadro com a média e o desvio recentes da PRÓPRIA câmera.
Hoje ele para de aprender enquanto `z >= CongelaAcima` (3,0) - quadro a quadro.
O que escapa é a **cauda** do evento: o carro que entra na garagem produz uns
segundos de z alto, que não são aprendidos, e depois dezenas de segundos de z
entre 0 e 3 - a manobra, a porta, a sombra -, que são. Essa cauda entra na
média e no desvio, e o desvio é o denominador do z.

Medido na chegada que motivou isto (`cam_lateral1`, 20/09/2026, 10:45):

| instante | quadro | média da régua | desvio (log) | z | destaque |
|---|---|---|---|---|---|
| 10:44:52, antes do carro | 2.092 B | 525 B | 0,414 | **3,20** | **3,09 → onset** |
| 10:45:51, a pessoa descendo | **3.592 B** | 942 B | 0,832 | 1,62 | **-0,87 → nada** |

A pessoa produziu quadros **71% maiores** que os que dispararam o onset um
minuto antes, e o gatilho os considerou normais.

## A candidata

`kleinberg-c`: o mesmo score, com uma diferença só - o congelamento ganha
**histerese própria**. Ele abre em `CongelaAcima`, como hoje, e só fecha depois
de `QuadrosParaDescongelar` quadros SEGUIDOS abaixo de `SaiDoEventoZ`. A cauda
inteira fica de fora do aprendizado.

Com `SaiDoEventoZ = CongelaAcima` e `QuadrosParaDescongelar = 1` ele é, por
construção, o score de hoje - e há teste que segura isso
(`TestCongeladoComHistereseDeUmQuadroEOOriginal`).

## Como a comparação foi feita

**Custo igual.** Cada função devolve um número numa escala própria; comparar as
duas no mesmo limiar mediria a escala. Cada uma teve o seu limiar procurado por
bisseção até emitir as mesmas 100 olhadas por hora por câmera (nível 5), e só
então elas se olharam.

**Corpus:** 18 câmera-dias, **379,9 horas**, as 9 câmeras, 2 dias cada
(18 e 19/09/2026). Balanceado de propósito: sem teto por câmera, a que tem mais
dias no corpus define o limiar de todas.

**Quem decide é código de produção:** o `Score`, o `Mecanismo`, o `Recortador`,
o `Detector` (o `dwnvr-detect` de verdade) e o `Marcador`. O corte dos pedaços
foi conferido byte a byte contra o que o mecanismo produz.

## Os limiares calibrados (nível 5, 100 olhadas/h por câmera)

| função | limiar | custo obtido |
|---|---|---|
| `kleinberg-p` (hoje) | 1,6882 | 99,79/h |
| `kleinberg-c:2.0:10` | 1,0967 | 100,35/h |
| `kleinberg-c:1.5:10` | 0,5911 | 99,85/h |

**O limiar de produção rende menos do que promete neste corpus:**

| limiar em produção | alvo | rende |
|---|---|---|
| 2,1312 (nível 5) | 100/h | **91,9/h** |
| 4,9695 (nível 4) | 50/h | **44,5/h** |

São 8% a 11% de orçamento não gasto. Não é o problema principal, mas é folga
que existe. Atenção: o limiar calibrado depende do corpus - com 1 dia por
câmera deu 1,2986, com 2 dias 1,6882, e o de produção, medido sobre mais horas,
é 2,1312. A comparação entre A e B vale porque as duas usam o MESMO corpus;
levar qualquer um destes números a produção exige um corpus pelo menos do
tamanho do que calibrou o de hoje.

## Onde cada uma gasta as olhadas

`kleinberg-p` (limiar 1,6882) contra `kleinberg-c:2.0:10` (limiar 1,0967):

| câmera | olhadas hoje | olhadas cand. | comuns | só hoje | só cand. |
|---|---|---|---|---|---|
| cam_cozinha | 5.206 | 3.473 | 2.435 | 2.771 | 1.038 |
| cam_frente | 4.887 | 5.162 | 3.985 | 902 | 1.177 |
| cam_fundo | 4.576 | 5.003 | 3.787 | 789 | 1.216 |
| cam_jardim | 5.328 | 5.736 | 4.306 | 1.022 | 1.430 |
| cam_lateral1 | 2.939 | 2.633 | 1.583 | 1.356 | 1.050 |
| cam_lateral2 | 2.414 | 3.017 | 1.926 | 488 | 1.091 |
| cam_porta | 3.878 | 3.844 | 2.847 | 1.031 | 997 |
| cam_portao | 4.171 | 4.646 | 3.626 | 545 | 1.020 |
| cam_quintal | 4.508 | 4.605 | 3.553 | 955 | 1.052 |
| **TOTAL** | **37.907** | **38.119** | **28.048** | **9.859** | **10.071** |

O custo fecha (0,6% de diferença) e as duas **divergem em 26,2% das olhadas**.
O limiar é global, então o custo só fecha no total: na `cam_cozinha` a
candidata gasta 33% menos, e em outras um pouco mais.

## O caso que motivou tudo

Com os dois limiares calibrados no mesmo corpus, no episódio de `cam_lateral1`:

| | onsets entre 10:44:30 e 10:47:00 |
|---|---|
| hoje | 10:44:51 · 10:45:00 · 10:46:39 |
| candidata | 10:44:51 · 10:45:00 · **10:45:49** · 10:46:37 |

A candidata dispara às 10:45:49,8 e manda olhar o quadro das 10:45:50,7. O
detector, nesse quadro, devolve **`person=0,841`** - acima do corte de 0,40, e
portanto **marca de `pessoa` na timeline**. A de hoje não dispara ali, nem com
o limiar recalibrado: o destaque da pessoa é 1,61 contra o limiar 1,69.

## O limite do desenho: o congelamento pode PRENDER

A curva de custo x limiar do `kleinberg-c` **satura**. Numa câmera agitada, se
o z nunca desce de `SaiDoEventoZ`, a régua para de aprender para sempre, o
destaque fica cronicamente alto e o intervalo da histerese quase não fecha -
menos onsets, não mais.

Com `SaiDoEventoZ = 1,0` e `QuadrosParaDescongelar = 10, 20 ou 30`, a função
**nem alcança as 100 olhadas/h do nível 5** em limiar nenhum. A calibração
avisa em vez de devolver número, mas é um risco de desenho: um parâmetro mal
escolhido cega a câmera em silêncio.

As combinações que alcançam o nível 5 com folga, no corpus completo, são as de
`SaiDoEventoZ >= 1,5` com no máximo 10 quadros, e as de `SaiDoEventoZ >= 2,0`.

### Quanto tempo a régua fica congelada

Medido por câmera-dia (`detect-test-v5/congelamento`), é a fração dos quadros
em que o score não aprendeu:

| câmera | `1,0` / 30 quadros | `2,0` / 10 quadros |
|---|---|---|
| cam_cozinha | **95,6% · 96,9%** | 67,5% · 52,1% |
| cam_frente | 64,9% · 60,2% | 20,8% · 15,6% |
| cam_fundo | 55,2% · 57,9% | 16,1% · 16,3% |
| cam_jardim | 74,5% · 56,4% | 20,1% · 17,7% |
| cam_lateral1 | **99,0% · 97,2%** | 44,7% · 64,0% |
| cam_lateral2 | 78,3% · 71,3% | 17,3% · 19,7% |
| cam_porta | **99,0% · 93,4%** | 48,4% · 35,7% |
| cam_portao | 58,1% · 58,1% | 13,2% · 15,6% |
| cam_quintal | 41,4% · 65,5% | 15,4% · 21,4% |

Com `1,0` e 30 quadros - o primeiro palpite, antes de medir - três câmeras
ficam com a régua congelada 95% a 99% do tempo. Isso não é "proteger a régua da
cauda do evento", é **desligar o aprendizado**. Por isso o padrão ficou em
`SaiDoEventoZ = 2,0` e `QuadrosParaDescongelar = 10`.

Mesmo assim, `cam_cozinha`, `cam_lateral1` e `cam_porta` passam 36% a 67% do
tempo sem aprender. **Isto não foi medido e precisa ser, antes de qualquer
adoção:** o que acontece no anoitecer, quando o bitrate da câmera muda de
patamar por causa do infravermelho. Régua congelada é régua que não acompanha
essa mudança.

## O placar: a candidata PERDE

`kleinberg-p` (limiar 1,6882) contra `kleinberg-c:2.0:10` (limiar 1,0967), no
mesmo custo, sobre as 379,9 horas:

| família | hoje | candidata | comuns | perdas | ganhos | **saldo** |
|---|---|---|---|---|---|---|
| **pessoa** | 1.018 | 900 | 678 | 340 | 222 | **-118** |
| veiculo | 689 | 642 | 567 | 122 | 75 | -47 |
| animal | 192 | 154 | 107 | 85 | 47 | -38 |
| **TOTAL** | **1.899** | **1.696** | 1.352 | 547 | 344 | **-203** |

O funil das duas, pelas mesmas olhadas por hora:

| | hoje | candidata |
|---|---|---|
| olhadas | 37.907 | 38.119 |
| sem pedaço | 496 | 449 |
| falhas do detector | 190 | 200 |
| olhadas sem objeto | 35.430 | 35.880 |
| olhadas com objeto | 1.791 | 1.590 |

**Ela perde na família que mais importa.** São 118 chegadas de `pessoa` a menos
por 380 horas - mais de uma por dia em nove câmeras.

### Por câmera: o estrago está onde a régua prende

| câmera | hoje | cand. | perdas | ganhos | saldo | congelamento |
|---|---|---|---|---|---|---|
| cam_cozinha | 336 | **138** | 231 | 33 | **-198** | 52-67% |
| cam_frente | 819 | 781 | 131 | 93 | -38 | 16-21% |
| cam_lateral1 | 86 | 69 | 47 | 30 | -17 | 45-64% |
| cam_portao | 24 | 21 | 11 | 8 | -3 | 13-16% |
| cam_fundo | 211 | 219 | 42 | 50 | +8 | 16% |
| cam_quintal | 32 | 34 | 7 | 9 | +2 | 15-21% |
| cam_lateral2 | 57 | 64 | 10 | 17 | +7 | 17-20% |
| cam_porta | 148 | 161 | 39 | 52 | +13 | 36-48% |
| cam_jardim | 186 | 209 | 29 | 52 | +23 | 18-20% |

A correlação é direta: onde o congelamento **prende** (cozinha, lateral1), a
candidata desaba. Onde ele fica em 15-20%, ela ganha um pouco. A `cam_cozinha`
sozinha responde por quase todo o saldo negativo: perde 198 marcas das 203.

E os ganhos são do tipo certo - `pessoa` com confiança alta (0,90 a 0,93), que
é exatamente o que a variante prometia. Só que as perdas são maiores.

## Conclusão: reprovada como está

O congelamento com histerese **resolve o episódio que o motivou** - na
`cam_lateral1` às 10:45, só ela marca a pessoa - e **entrega menos no
agregado**, pelo mesmo preço.

A causa é a mesma nas duas pontas: ele para de aprender. Isso protege a régua
da cauda de UM evento e, numa câmera movimentada, a deixa presa no passado.

**O que não se sabe, e é o próximo passo natural:** uma variante que congele
com um TETO - por exemplo, no máximo N quadros congelados seguidos, voltando a
aprender à força depois disso. Ela teria o ganho da cauda curta sem o risco da
régua presa. A bancada já roda um candidato novo com um comando.

O que **não** vale a pena mexer, porque a medição mostrou que o efeito é
pequeno: o limiar de produção, que rende 91,9/h contra os 100/h nominais.
