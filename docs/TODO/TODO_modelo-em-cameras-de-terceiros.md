# TODO - medir o modelo int8 em câmeras que não são as do autor

**Status: pergunta aberta, nada medido.**

## O problema

O `dwnvr-detect/modelo/rfdetr-n_512x288_int8.onnx` foi quantizado para int8
calibrando em quadros das câmeras do autor: metade de dia, metade à noite em
infravermelho, metade com objeto, metade vazia (o critério está em
[`dwnvr-detect/modelo/README.md`](../../dwnvr-detect/modelo/README.md)). Os
pesos, que são o que o modelo sabe, vêm do RF-DETR Nano treinado no COCO e são
os mesmos para todo mundo. A calibração só escolhe a faixa numérica de cada
tensor.

Todo número que existe sobre esse arquivo saiu dessas câmeras. O principal: o
int8 custa **2 pontos** de acerto contra o fp32, medidos em quadros que ficaram
fora da calibração. Ninguém sabe se isso vale na câmera de outra pessoa.

## Onde pode não valer

- **Imagem de outra natureza:** noite colorida (câmeras "full color", com luz
  branca), olho de peixe ou panorâmica, térmica, interna com contraluz forte.
- **Proporção que não é 16:9:** a entrada é 512x288 e o `servidor.py` estica o
  quadro sem letterbox. Uma câmera 4:3 chega deformada. Isso não é da
  calibração - vale para o fp32 também - e também nunca foi medido.

## Como medir

1. Vídeo público de câmera de segurança: puxar pelo YouTube com `yt-dlp`, de
   dia e de noite, incluindo uma câmera 4:3 e uma "full color".
2. Rodar os mesmos quadros no int8 e no fp32, que o `exporta.py` gera junto.
3. Contra o quê: o fp32 serve de referência para a pergunta da calibração, mas
   não para a do 4:3. Para essa, rotular à mão um punhado de quadros.

Se a diferença ficar perto dos 2 pontos, a pergunta está respondida e o int8
continua sendo o padrão. Se desabar, as saídas são publicar o fp32 como
alternativa documentada ou recalibrar com uma amostra mais variada (ver
[`TODO_recalibrar-modelo-com-imagens-publicas.md`](TODO_recalibrar-modelo-com-imagens-publicas.md)).
