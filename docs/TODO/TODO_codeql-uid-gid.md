# TODO: os dois alertas do CodeQL no UID e no GID do Diagnóstico

O CodeQL acusa `go/incorrect-integer-conversion` (severidade alta) em
`internal/api/diagnostics_process.go`, no `case "Uid"` e no `case "Gid"`: o
número sai do `strconv.ParseInt(..., 64)` e vira `int` sem conferir o teto.

- Já existia antes da conversão para en-US: são os alertas #19 e #20, abertos
  na `main` contra o `diagnostico_processo.go`. No PR #86 o arquivo só mudou de
  nome, e o CodeQL reabriu os mesmos dois como #84 e #85.
- Risco prático baixo: o valor vem do `/proc/self/status` do próprio processo,
  e nas plataformas da imagem (amd64 e arm64) `int` tem 64 bits. O alerta é
  real em plataforma de 32 bits.
- O conserto é pequeno: ler o UID e o GID com `strconv.ParseUint(..., 10, 32)`
  (ou conferir o teto antes do `int(n)`), sem mudar o que o Diagnóstico mostra.

Ficou fora do PR #86 porque aquele PR promete "sem mudança de comportamento".
Combinado em 11/11/2026: resolver depois, num PR próprio, e fechar os quatro
alertas (#19, #20, #84 e #85).
