# TODO - o que fica para depois do alerta de detecção

**Status: sem data (03/11/2026).** O alerta de detecção por câmera, com as
áreas, as famílias, os horários e o e-mail, entrou no PR #44. O que está aqui
ficou de fora dele, e cada item vira plano próprio quando chegar a vez. O
alerta como ele é hoje está em [`deteccao.md`](../deteccao.md#o-alerta) e em
[`configuracao.md`](../configuracao.md#alerta-por-e-mail).

## O que fica para depois

- **Os ajustes de layout por tamanho de tela**, numa lista do mantenedor,
  vistos no teste do filtro por área e das áreas da câmera (21 e 22/10/2026).
  O layout já está quase todo certo, e a lista só se escreve depois que o
  PR #44 for aceito (decisão do mantenedor, 02/11/2026).
- **Cada pessoa escolhe as câmeras** (decisão do mantenedor): o e-mail na
  Minha conta e as câmeras de que ela quer alerta, somados à lista global sem
  repetir endereço. O campo de e-mail no `usuarios.json` já existe (PR #50,
  que o PR #51 usa também para entrar), mas só o admin o preenche, na tela
  Usuários, e ninguém confirma o endereço. Falta a pessoa trocá-lo na Minha
  conta e, se ela mesma digitar, um e-mail de confirmação, para não guardar
  endereço digitado errado.
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
- **Notificação do navegador (push):** exige service worker, que o dwnvr evita
  de propósito (ver "Instalável como app, sem service worker" no
  `web/README.md`).
- **O silêncio**, se o alerta incomodar no uso: ver a seção abaixo.

## O silêncio, medido e adiado

Decidido em 22/10/2026: o alerta sai sem silêncio. O mantenedor volta a este
assunto só se o alerta incomodar no uso. Fica aqui o que já se sabe, para não
refazer.

**O problema.** A marca é de chegada (ver
[o rastreio](../deteccao.md#carro-parado-não-vira-marca-a-cada-olhada)): quem
anda pela cena muda de lugar e marca de novo a cada olhada. Uma pessoa 20
minutos no quintal, marcada a cada minuto, são uns 20 alertas sobre ela.

**O que o silêncio faz.** Depois de um alerta, a mesma câmera e família não
alertam de novo por um tempo. A detecção segue gravada em Detecções e na
timeline: só o aviso não sai, e o que ficou de fora é contado. É por
câmera e família: a pessoa no Portão não cala a do Fundo, e o gato de um minuto
atrás não cala a pessoa de agora.

**As duas formas de contar o tempo:**

| Forma      | Como conta                                | A favor                    | Contra                                                                         |
| ---------- | ----------------------------------------- | -------------------------- | ------------------------------------------------------------------------------ |
| fixo       | o tempo corre a partir do alerta          | lembra que a cena continua | a presença longa vira um alerta a cada N minutos                               |
| deslizante | cada marca que cai no silêncio o reinicia | um alerta por episódio     | numa câmera sempre movimentada ele nunca fecha, e quem chega no meio não avisa |

E a duração: mais longa manda menos alerta, e cala a segunda chegada que cair
dentro dela.

**Medido em 22/10/2026**, sobre 15 a 19/10/2026 da instalação de teste, no
nível 4, antes de o padrão virar 5: `pessoa` nas 9 câmeras, na imagem inteira,
o dia todo. Uma olhada é o conjunto das marcas de objeto de um mesmo
`instanteMs` no `eventos/{dia}.ndjson`, e o teste de área é o do
`internal/area`.

| Silêncio                      | Alertas por dia, somadas as 9 câmeras |
| ----------------------------- | ------------------------------------- |
| nenhum                        | 629                                   |
| fixo, 1 min                   | 430                                   |
| fixo, 5 min                   | 247                                   |
| fixo, 15 min                  | 146                                   |
| deslizante, 5 min             | 173                                   |
| deslizante, 15 min            | 85                                    |
| fixo, 5 min, só das 22h às 6h | 12,6                                  |

- De dia, numa casa com gente, o silêncio não segura o volume: com 15 min
  fixo, as três câmeras mais movimentadas ainda dão de 27 a 37 alertas por dia
  cada. De dia, o freio é a câmera, a área e o horário.
- De noite o volume é pequeno, e o silêncio só segura a rajada sobre a mesma
  pessoa.
- Só nas áreas de teste de 4 câmeras: 113 por dia com 5 min fixo, e 70 com
  15 min.
- O nível 5 marca mais chegadas que o 4, e os números devem subir.

A recomendação, na época, foi o deslizante com 5 min. Sem silêncio, o único
freio do volume de e-mails é o teto de envio (ver os limites em
[O alerta](../deteccao.md#o-alerta)).
