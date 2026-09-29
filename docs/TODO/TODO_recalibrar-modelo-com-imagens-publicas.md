# TODO - recalibrar o modelo com imagens públicas

**Status: não implementado.**

## O problema

O `.onnx` publicado foi calibrado com quadros das câmeras do autor, que não
estão no repositório (ver
[`dwnvr-detect/modelo/README.md`](../../dwnvr-detect/modelo/README.md)).
Funciona, mas tem dois defeitos:

- **Ninguém consegue reproduzir o arquivo publicado.** O `exporta.py` roda com
  qualquer pasta de imagens, mas o `.onnx` que sai dele não é o mesmo, e as
  medições do publicado não valem para ele. Só o autor, com os quadros dele,
  refaz o publicado (ver "O que se mediu em 29/09").
- **A amostra é de uma instalação só.** Câmeras de um modelo, uma casa, uma
  iluminação. É a pergunta do
  [`TODO_modelo-em-cameras-de-terceiros.md`](TODO_modelo-em-cameras-de-terceiros.md).

## A ideia

Montar a amostra de calibração só com imagens públicas, de licença que permita
redistribuir, e versionar a lista delas (ou as próprias imagens, se forem
poucas e pequenas) junto do `exporta.py`. Assim o `.onnx` publicado passa a
ser reproduzível por qualquer um.

O critério da amostra continua o mesmo do README do modelo: dividida entre
câmeras diferentes, metade de dia e metade de noite em infravermelho, metade
com objeto e metade sem.

Fontes possíveis: vídeos públicos de câmera de segurança (YouTube, com a
licença de cada um conferida) e datasets abertos de vigilância.

## Antes de trocar o publicado

- Medir o int8 novo contra o atual e contra o fp32, nos mesmos quadros e fora
  das duas calibrações. O novo só substitui o atual se não perder acerto.
- Conferir de dia e de noite separadamente: calibração errada perde objeto
  justamente à noite.
- Separar `person` do agregado: é a classe que importa, e o holdout atual tem
  pouca (24 achados do fp32 no piso 0,20).
- Trocar junto o sha256 do int8 no README do modelo: sem o hash, ninguém
  percebe que gerou outro arquivo (ver abaixo).

## O que se mediu em 29/09

Com os quadros originais da calibração (163) e do holdout (66), que continuam
fora do repositório, em PNG sem perda:

1. **O pipeline é determinístico.** O `exporta.py` de hoje, com o `torch`
   2.11.0 do build de CUDA (`+cu128`), que foi o que gerou o publicado, refaz
   o fp32 e o int8 publicados byte a byte. O int8 também sai idêntico
   quantizando de novo o fp32 original: a quantização não sorteia nada.
2. **A versão do `torch` não muda o resultado.** O 2.13.0 do build de CUDA
   (`+cu130`, o padrão do PyPI) refaz o int8 publicado byte a byte; no fp32, os
   760 tensores são idênticos e só muda a versão gravada no arquivo. E o 2.11.0
   e o 2.13.0 do build de CPU dão, entre si, o mesmo int8 byte a byte.
3. **O build muda.** O fp32 do build de CPU difere do de CUDA num tensor só, o
   `position_embeddings` do backbone, por 5,6e-8: o último bit do float32. O
   resto dos 760 tensores e o grafo são idênticos.
4. **O int8 amplifica essa diferença.** No piso 0,20, ~17% dos achados trocam
   entre o int8 publicado e o do build de CPU. Contra o fp32, os dois ficam
   empatados:

| piso | casados com o fp32 (publicado / CPU) | só `person` (publicado / CPU) |
| --- | --- | --- |
| 0,20 | 449 / 458 de 601 | 18 / 17 de 24 |
| 0,40 | 128 / 131 de 146 | 5 / 5 de 6 |
| 0,60 | 55 / 53 de 68 | 4 / 4 de 5 |

O que isso muda:

- **O caminho do README não gerava o publicado.** Ele instalava o `torch` de
  CPU, que dá um int8 de mesma qualidade e outro arquivo, e sem o hash ninguém
  percebia. Desde 29/09 o README instala o do PyPI e traz o sha256 do int8.
- **Reproduzir exige o mesmo build, e talvez a mesma CPU.** Só foi testado
  nesta máquina. Não se sabe se o build de CUDA dá o mesmo arquivo em outra CPU
  (AVX2 contra AVX-512, por exemplo). É a pergunta que falta para prometer
  reprodução byte a byte.
- **Subir o `torch` foi de graça.** Em 29/09 ele foi para 2.13.0, o que fecha o
  alerta do Dependabot (GHSA-rrmf-rvhw-rf47), e o `torchvision` foi junto para
  0.28.0: o 0.26.0 exige `torch==2.11.0`, e o bump só do `torch`, que era o PR
  #6, quebraria o `pip install`.
