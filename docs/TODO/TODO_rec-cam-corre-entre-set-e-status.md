# TODO - o `rec.cam` é escrito e lido sob locks diferentes

**Status: não corrigido, por escopo.** Não quebrou nada que se saiba, e mexer
nisso é mexer no ciclo de vida do recorder.

## A corrida

`Manager.Set` (`internal/recorder/manager.go`), quando a mudança não exige
reconectar - trocar o nome, a cota -, escreve a câmera nova por cima da antiga:

```go
m.mu.Lock()
old.rec.cam = cam
m.mu.Unlock()
```

O lock é o do **Manager**. Quem lê o mesmo campo usa outro, ou nenhum:

- `Recorder.Status()` lê `r.cam.ID`, `r.cam.Name`, `r.cam.QuotaMB`... sob o
  `r.mu` do **Recorder**
- `Recorder.session()` lê `r.cam.ID`, `r.cam.Audio` e `r.cam.StallSeconds`
  sem lock nenhum, a cada reconexão
- `silenceLimitLocked()` lê `r.cam.SegmentSeconds` sob o `r.mu`

Dois locks diferentes não se excluem. Pelo modelo de memória do Go isso é
corrida de dados, e o `-race` a acusa num teste de 200 `Set` contra 200
`Status`.

## O que isso causa

Na prática, quase nada visível: `config.Camera` é uma struct de meia dúzia de
campos, e o pior caso é o `/api/health` ler um nome ou uma cota no meio da
troca e mostrar o valor velho por três segundos. Não há ponteiro sendo
liberado, então não há crash.

O risco é outro: é o tipo de corrida que fica inofensiva até alguém acrescentar
um campo que não é inofensivo - um slice, um mapa, um ponteiro que outra
goroutine segue e que o laço de gravação lê de `r.cam`.

## O conserto provável

Escrever `rec.cam` sob o `rec.mu`, que é o lock que o `Status()` já usa, e
fazer o `session()` copiar os três campos sob o mesmo lock no começo de cada
conexão. É pouca linha. O cuidado é não segurar o `rec.mu` durante o
`OpenStream`, que pode demorar até o `ResponseHeaderTimeout`.

## Como reproduzir

Um teste no pacote `recorder` que sobe um `Manager` com uma câmera, chama
`m.Status()` em laço numa goroutine e `m.Set()` trocando só o nome em outra, e
roda com `go test -race`. Não fica commitado porque, sem o conserto, ele
falharia na CI.
