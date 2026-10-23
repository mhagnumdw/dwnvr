# TODO - uma olhada de aquecimento por câmera ao subir

**Status: desenhada e não implementada (22/10/2026).** Pesados os prós e os
contras, o mantenedor está um pouco mais inclinado a NÃO fazer.

## O problema

O rastreio de lugares ocupados (`internal/detect/marcador.go`) vive em memória.
Ele sobrevive à reconexão do stream de propósito, mas some quando o recorder é
recriado: o dwnvr reinicia, a câmera muda áudio, segmento ou stall
(`recordingParamsChanged`), a câmera é desligada e ligada, as gravações dela
são apagadas (`Manager.Pause`) ou a detecção é ligada com o dwnvr no ar.

Na primeira olhada depois disso, o que está parado na cena vira chegada: o
carro na garagem marca na timeline e, com o alerta das áreas, avisaria a cada
reinício. É a opção b de "O objeto parado depois de um reinício", na Etapa 3 de
[`TODO_areas-e-alertas-de-deteccao.md`](TODO_areas-e-alertas-de-deteccao.md),
que ficou com a a (aceitar e medir no uso).

## O desenho

Quando o marcador nasce, a câmera manda ao detector de objetos uma olhada sem
onset, que ensina o rastreio e não marca nada.

- **O pedaço:** o `Recortador` ganha um corte sem onset (init + GOP guardado até
  o último quadro). Chamado logo depois de um frame I, é só esse frame. É a
  mesma peça de que a
  [`TODO_olhada-forcada-sem-onset.md`](TODO_olhada-forcada-sem-onset.md)
  precisa.
- **Quando:** no primeiro frame I depois de `EsperaDoAquecimentoMs` (10 s) da
  conexão. Na instalação de teste, num boot das duas imagens em 22/10/2026, as
  câmeras conectaram às 15:49:21 e o detector de objetos ficou pronto às
  15:49:26. Sem a espera, todo boot completo daria "detector de objetos fora do
  ar" e pausaria a fila 30 s. Os 10 s ainda vêm antes do primeiro onset
  possível (o gatilho precisa de 300 quadros, 10 s a 30 fps).
- **Na reconexão, não:** o rastreio sobrevive a ela.
- **A olhada de aquecimento:**
  - respondida: passa pelo `Marcador.Olha` e as marcas são descartadas;
  - o detector de objetos não respondeu: nova tentativa no frame I seguinte,
    sem limite. Não custa CPU, porque a fila já pausa 30 s a cada falha;
  - pedaço recusado: desiste, e a câmera fica como hoje;
  - o rastreio já aprendeu com uma olhada real: é ignorada.
- **A olhada real que chega antes do aquecimento** marca normalmente e ensina o
  rastreio. Só acontece com o detector de objetos fora do ar mais de ~30 s no
  boot.
- **Fora do funil:** a olhada de aquecimento não tem onset, e as fatias seguem
  fechando a conta com os onsets. Ela não passa pelo `ofereceA`, que conta
  `cortados` e `descartados`.
- **Para conferir:** só uma linha Info no log por câmera ("rastreio aquecido",
  com o que foi visto por família). O JSON e a tela não mudam.
- **Não é contrato trocável:** não há algoritmo alternativo a medir, só "com"
  ou "sem".

## Os pontos negativos

1. **Quem chega bem na hora do boot não marca.** A pessoa que está na cena é
   aprendida como "já estava lá" e só volta a marcar depois de
   `FaltasParaLiberar` (5) olhadas seguidas sem ela.
2. **Atraso no boot.** Com 9 câmeras, as primeiras marcas reais esperam até
   ~45 s atrás dos aquecimentos (3,6 a 6,5 s por olhada num Orange Pi Zero 3).
   Cada aquecimento ocupa uma das `PedacosPorCamera` (2) vagas da câmera.
3. **Os 10 s são medidos num hardware só.** Numa placa mais lenta, o "fora do
   ar" falso volta em todo boot. A nova tentativa conserta o rastreio, mas o
   Aviso continua acendendo.
4. **O caso raro continua.** Com o detector de objetos fora do ar mais de ~30 s
   no boot, uma olhada real pode chegar antes do aquecimento e marcar o carro
   parado.
5. **O recusado desiste.** Se o frame I do aquecimento não decodificar, a
   câmera fica exposta como hoje.
6. **Cena calma prende o lugar aprendido.** Numa câmera com poucos onsets, o
   lugar aprendido no boot pode ficar ocupado por horas. É o mesmo problema da
   [`TODO_olhada-forcada-sem-onset.md`](TODO_olhada-forcada-sem-onset.md), que
   passa a valer também para o que o aquecimento ensinou.
7. **Só aparece no log.** Para saber se uma câmera aqueceu, é preciso ler o
   log; o Diagnóstico não mostra.
8. **A média de análise do Diagnóstico distorce no boot.** O aquecimento é um
   quadro só e decodifica mais rápido, então essa média fica mais baixa por um
   ou dois minutos.
9. **Pedaço do recorder antigo cai no novo.** Quando a câmera é recriada, um
   pedaço velho que ainda estava na fila é entregue ao recorder novo
   (`Manager.olhou` procura pelo ID). Ele pode marcar antes do aquecimento ou
   ensinar o rastreio com um quadro de antes.

Os que pesam mais são o 1 e o 6. Os outros são raros ou só de visibilidade.

## Antes de implementar

- Medir no uso, com a opção a, quantas marcas falsas os reinícios deixam
  (timeline e alertas). Implementar só se elas incomodarem mais que os pontos
  negativos acima.
