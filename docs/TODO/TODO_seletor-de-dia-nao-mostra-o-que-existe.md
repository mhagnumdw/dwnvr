# TODO — o seletor de dia em Gravações não diz onde há gravação

Levantado em 12/08/2026, na mesma conversa que trouxe a retenção real para as
telas de Câmeras e Diagnóstico. **Fora do escopo daquela tarefa por decisão** —
é atividade própria.

## O problema

Em `web/src/routes/Recordings.svelte:89` o campo de data tem `max={dayKey()}` mas
**não tem `min`**, e nada indica quais dias têm material. Para descobrir o fundo
do histórico só clicando "‹" repetidamente até aparecer "sem gravação" — e mesmo
aí não dá para saber se aquilo é o fim do disco ou um buraco no meio.

Agora que o Diagnóstico diz "retido: 12 dias 4h", a tela onde essa informação
seria usada é justamente a que não a tem.

## O que já existe pronto

- `GET /api/rec/days` (`internal/api/recordings.go:27`) devolve os `DaySummary`
  da câmera — `Day`, `Count`, `Bytes`, `FirstMs`, `LastMs`.
- `api.days(cam, from, to)` (`web/src/lib/api.js:69`) já está escrito e **não é
  chamado de lugar nenhum** da interface.

Ou seja: o backend inteiro deste TODO já está feito e sem uso.

## Como implementar

1. Carregar `api.days` ao trocar de câmera, no mesmo `$effect` que já recarrega a
   timeline (`Recordings.svelte:43`), guardando um `Set` de dias com gravação.
2. `min` no input de data com o dia mais antigo, para o próprio navegador barrar
   a navegação para fora do que existe.
3. Desabilitar o "‹" ao chegar no mais antigo, como o "›" já faz com hoje
   (`Recordings.svelte:90`).
4. As "bolinhas" de dia com gravação — o comentário em `recordings.go:26` já
   promete isso ("as bolinhas do calendário") e elas nunca existiram. O
   `<input type="date">` nativo **não** permite marcar dias, então isto exige um
   calendário próprio. É a parte cara, e vale medir se compensa: os itens 2 e 3
   já resolvem a maior parte da dor sozinhos.

## Vale a pena?

**Sim, os itens 1–3.** São pequenos, usam endpoint que já existe e eliminam a
navegação às cegas. O item 4 é uma tarefa de UI de verdade e deve ser decidido à
parte.
