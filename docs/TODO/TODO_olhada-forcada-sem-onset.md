# TODO - forçar uma olhada depois de X tempo sem olhada

**Status: ideia, não medida.**

## O problema

O rastreio de lugares ocupados (`internal/detect/marcador.go`) só libera um
lugar depois de `FaltasParaLiberar` (5) olhadas seguidas sem ver o objeto. E
olhada só acontece quando o mecanismo emite um onset.

Se o carro sai e volta sem gerar 5 olhadas no meio, o lugar continua ocupado,
e **a volta dele não marca na timeline**. É mais provável em câmera de cena
calma, que passa muito tempo sem onset.

## A ideia

Se uma câmera ficar X tempo (configurável) sem nenhuma olhada, forçar uma. As
olhadas forçadas vão acumulando as faltas que liberam o lugar.

## Antes de implementar

- **Medir quantas chegadas se perdem por esse motivo.** O rastreio inteiro
  custa uma fatia pequena das chegadas de `veiculo` (alguns por cento, nas
  gravações em que ele foi medido), e esse caso é só uma parte dela, que ainda
  não foi separada.
- **O custo:** cada olhada leva ~6,5 s num núcleo de um Orange Pi Zero 3, e
  divide a fila com as olhadas de verdade. Uma por minuto dá 1.440 por câmera
  por dia, 60 por hora - mais que o nível 4 de sensibilidade inteiro, que
  custa 50.
- Implementar só se a perda medida pagar esse custo.
