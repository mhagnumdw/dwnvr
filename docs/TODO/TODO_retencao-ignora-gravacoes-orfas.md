# TODO — a retenção não enxerga as gravações de câmeras removidas

Levantado em 11/08/2026, enquanto se planejava o botão de apagar gravações.
Não é um defeito que quebra alguma coisa: é uma **inversão de prioridade** que só
apareceria numa investigação de espaço em disco, provavelmente meses depois.

**Status: não implementado, por decisão.** A entrega do dia deu ao usuário como
apagar as órfãs com um clique, o que cobre o caso prático. O que segue é o que
falta para o dwnvr se defender sozinho.

## O descompasso

`internal/retention/retention.go:10-13` promete:

> Encher o disco é pior que perder gravação antiga, então esse limite ignora as
> cotas individuais e evicta o segmento mais antigo **de todo o sistema**.

E `retention.go:129-130` repete: "evicta o dia mais antigo de **qualquer**
câmera". Mas a implementação (`retention.go:170-181`) é:

```go
func (m *Manager) oldestDay() (cam, day string, ok bool) {
	for _, c := range m.resolved() {   // == mgr.Cameras()
```

`m.resolved()` são só as câmeras **cadastradas**. Remover uma câmera a tira de
`m.cams` (`internal/recorder/manager.go:149-155`) e o diretório dela continua no
disco, invisível para esse laço. "Todo o sistema" é, na prática, "tudo que ainda
está no `cameras.json`".

## O que isso causa

Disco de 128 GB, `minFreeMB: 2048`, 8 câmeras gravando. Uma `cam_garagem` foi
removida no mês passado com 20 GB preservados.

1. O disco aperta: sobram 1,8 GB livres.
2. `enforceFreeSpace` acorda, chama `oldestDay()`, que varre as 8 vivas.
3. Apaga o dia mais antigo de uma câmera **que está no ar**.

A rede de segurança funciona — libera espaço —, mas cobra da fonte errada.
**Perde-se gravação viva enquanto 20 GB de vídeo morto ficam intocados.**

No extremo, com as vivas já drenadas e o disco ainda apertado, cai em
`retention.go:147-151`:

```
disco abaixo do mínimo livre, mas não há mais nada a evictar
```

que é literalmente falso, com 20 GB ali do lado.

### Efeito colateral no diagnóstico

`internal/api/server.go:150-161` soma `DiskBytes` só das cadastradas para montar
`disk.dwnvrBytes`. O espaço órfão não entra nessa conta mas ocupa o disco, então
na tela de Diagnóstico ele aparece como **consumo de fora do dwnvr** — quem
investigar vai procurar no Pi um log gigante que não existe.

## O que **não** é defeito

Cota por câmera (`enforceQuota`) e `maxDays` (`enforceMaxDays`) ignorarem órfãs
está certo, e não deve mudar: esses limites são configuração *da* câmera. Câmera
removida não tem cota. O buraco é só no terceiro limite, que não é sobre
configuração de câmera — é sobre o disco, e o disco não sabe que a câmera foi
removida.

## Três saídas, discutidas na hora

1. **Órfã primeiro, só na emergência.** `enforceFreeSpace` drena as órfãs antes
   de tocar em câmera no ar. Só muda quem é a vítima no momento em que a retenção
   já ia apagar vídeo de qualquer jeito. Foi a recomendação.
2. **Órfã na fila normal, por idade.** `oldestDay()` varre tudo e o critério
   segue sendo a data. Menos código, cumpre o que o comentário promete, mas
   mantém o caso ruim: órfã de ontem sobrevive a gravação viva de anteontem.
3. **Manter como está.** Foi o escolhido para 11/08/2026.

## Contra-argumento honesto

Qualquer uma das duas primeiras faz o dwnvr **apagar sozinho, sem clique nenhum,
gravação que o usuário deliberadamente escolheu preservar** ao remover a câmera
sem marcar "apagar também as gravações". É exatamente o tipo de destruição
implícita que o resto do projeto evita de propósito
(`internal/api/cameras.go`, doc de `handleDeleteCamera`).

A defesa da saída 1 é que ela só age quando a alternativa é apagar gravação viva
— ou seja, não cria uma destruição nova, só escolhe melhor entre duas que já
iam acontecer.

## Como implementar, se um dia valer

A peça cara já existe: **`store.Orphans(registered)`**
(`internal/store/store.go`), escrita em 11/08/2026 para a listagem da tela, lê o
tamanho dos arquivos de índice em vez de fazer walk pelos segmentos.

1. Dar ao `retention.Manager` acesso ao conjunto de IDs cadastrados — ele já
   recebe `cameras func() []config.Camera`, então é só derivar o mapa.
2. Em `enforceFreeSpace`, antes de `oldestDay()`, tentar
   `s.store.Orphans(...)` e evictar o dia mais antigo entre elas (`DropDay` já
   serve; `Purge` seria grosso demais numa passada de minuto em minuto).
3. Logar com verbo diferente do caso normal — apagar órfã e apagar câmera viva
   não podem sair iguais no journal.
4. Aproveitar para somar as órfãs em `disk.dwnvrBytes`, ou expor um campo à
   parte, para que a tela de Diagnóstico pare de creditar esse espaço a
   "outros".

Estimativa: ~30 linhas mais um `retention_test.go`, que hoje não existe — o
pacote não tem teste nenhum, então esse é o custo real escondido.

## Vale a pena?

**Não agora.** Com a listagem de "Gravações sem câmera" na tela de Câmeras, os GB
órfãos deixaram de ser invisíveis, que era o pior do problema. O usuário vê o
tamanho e apaga quando quiser.

O critério para reabrir é um só: **o dwnvr precisa se defender de um disco cheio
sem ninguém olhando?** Se a instalação é vigiada por alguém que abre a tela de
vez em quando, a listagem basta. Se ela roda esquecida num armário por meses —
que é o caso de uso de um NVR —, aí a rede de segurança precisa ser de verdade, e
a saída 1 passa a valer o custo.
