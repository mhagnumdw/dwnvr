# TODO - recalibrar o modelo com imagens públicas

**Status: não implementado.**

## O problema

O `.onnx` publicado foi calibrado com quadros das câmeras do autor, que não
estão no repositório (ver
[`dwnvr-detect/modelo/README.md`](../../dwnvr-detect/modelo/README.md)).
Funciona, mas tem dois defeitos:

- **Ninguém consegue reproduzir o arquivo publicado.** O `exporta.py` roda com
  qualquer pasta de imagens, mas o `.onnx` que sai dele não é o mesmo, e as
  medições do publicado não valem para ele.
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
