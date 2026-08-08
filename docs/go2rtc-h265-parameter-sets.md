# H265: o go2rtc grava parameter sets falsos no container

Descoberto em 08/08/2026 ao trocar a `cam_portao` de `onvif1` (1080p) para
`onvif2` (640x360). **Isto corrige uma conclusão anterior desta documentação**,
que tratava o assunto como um erro cosmético de leitura de SPS.

## O que acontece

Para trilhas H265, o `pkg/mp4/muxer.go` do go2rtc faz:

```go
vps, sps, pps := h265.GetParameterSet(codec.FmtpLine)
if len(sps) == 0 {
    sps = []byte{0x42, 0x01, 0x01, 0x01, 0x40, ...} // SPS hardcoded
}
```

Nas câmeras Yoosee o `FmtpLine` **não** traz os parameter sets, então
`GetParameterSet` devolve vazio e o go2rtc escreve o **SPS embutido no próprio
código** dentro do `hvcC`. Esse SPS descreve 2560x1440 — daí a resolução errada
que aparece no `ffprobe`.

Confirmado byte a byte: a sequência hardcoded do go2rtc está presente,
literalmente, no init segment gravado pelo dwnvr.

Os parameter sets verdadeiros chegam **in-band**, dentro dos samples. É por isso
que a decodificação sai certa apesar do container mentir.

## Evidência

`cam_portao` depois de trocar para `onvif2`:

| | Valor |
|---|---|
| Resolução no container | 2560x1440 |
| **Resolução decodificada de verdade** | **640x360** |
| Taxa | 46-55 kbps (contra ~928 kbps em `onvif1`) |
| Framerate | 10 fps (o `onvif1` é 15 fps) |

E o teste que fecha o caso: mudar a resolução da câmera **não mudou o hash do
init** — porque o init nunca conteve o SPS real, só o dummy, que é o mesmo
sempre.

## Consequência 1 — `hev1` é obrigatório, não cosmético

O 4CC da sample entry é `hev1`, que permite parameter sets in-band. `hvc1`
afirma o oposto: que eles estão **apenas** na sample entry.

Como aqui a sample entry contém dummies, **converter `hev1` para `hvc1`
quebraria a reprodução**. A ideia de trocar esse 4CC para agradar o Safari — que
aceita `hvc1` e não `hev1` — está descartada para estas câmeras. Quem precisar
de Safari terá que resolver por outro caminho.

## Consequência 2 — o hash do init não detecta troca de codec em H265

O `Gen` do índice é o hash do init. Para H264 isso funciona: a `cam_lateral1`
trocou de `onvif1` para `onvif2` e abriu uma geração nova (`ead03cc7ac5a` →
`27dcb8115adf`), com os dois inits convivendo no disco, exatamente como
projetado.

Para H265 o mecanismo é cego: como o init é sempre o mesmo dummy, a
`cam_portao` manteve `4edbc50d8e70` ao mudar de 1080p para 640x360.

**Isso não quebra a reprodução**, porque o decoder usa os parameter sets
in-band — verificado decodificando de verdade um segmento de 640x360 gravado
com o init dummy. O efeito é que, para H265, `Gen` não significa "mesma
configuração de codec"; significa apenas "mesmo conjunto de trilhas".

Onde isso pode incomodar: uma exportação que atravesse uma troca de resolução
gera um MP4 cujo container descreve uma resolução e cujo bitstream muda no meio.
Players seguem o in-band e costumam lidar bem, mas é um caso a testar quando a
exportação for implementada.

Detectar a troca de verdade exigiria comparar os parameter sets in-band, ou
seja, percorrer NAL units dentro dos samples — trabalho real que não se paga
agora, já que a reprodução não é afetada.

## Não confundir com perda de dados

O `ffmpeg` emite `Could not find ref with POC N` esporadicamente nestas
gravações. Não é o dwnvr perdendo dados: a taxa de frames dos segmentos gravados
é idêntica à da captura feita direto do go2rtc.

| Origem | fps |
|---|---|
| Direto do go2rtc | 10,02 |
| Segmentos do dwnvr | 9,94 · 9,96 · 10,00 · 9,99 |

A causa é perda de pacote UDP no enlace com a câmera (as fontes usam
`#transport=udp`), que atravessa os dois caminhos igualmente.
