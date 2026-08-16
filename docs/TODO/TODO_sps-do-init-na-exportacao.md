# TODO - corrigir o SPS falso do init na exportação

Investigado em 09/08/2026 na instalação real, com as 9 câmeras em produção.

Fecha a pergunta que `docs/go2rtc-h265-parameter-sets.md` deixou em aberto na
linha "é um caso a testar quando a exportação for implementada". A causa raiz
está lá e não se repete aqui: o go2rtc escreve um SPS **hardcoded** no `hvcC`
quando o `FmtpLine` da câmera não traz os parameter sets.

**Status: não implementado, e a recomendação é adiar.** Os motivos estão no
final.

## O que foi confirmado

A exportação herda a mentira. `handleExport` (`internal/api/recordings.go:297`)
escreve `initBytes` verbatim no topo do arquivo e emenda os fragmentos, então o
MP4 entregue ao usuário carrega o SPS dummy.

`cam_portao`, janela de 2 min, arquivo baixado e medido com ferramenta externa:

| | |
|---|---|
| Container (`mediainfo`) | 2560x1440 |
| **Frame decodificado** | **640x360** |

### Alcance: 7 das 9 câmeras

| Câmera | Codec | Container | Frame real | |
|---|---|---|---|---|
| cozinha, frente, fundo, jardim, quintal | HEVC | 2560x1440 | 1920x1080 | mente |
| cam_portao | HEVC | 2560x1440 | 640x360 | mente |
| cam_lateral1 | AVC | 320x180 | 640x360 | mente (ao contrário) |
| cam_lateral2, cam_porta | AVC | 1280x720 | 1280x720 | ok |

**Não meça isto com `ffprobe`.** O parser de H264 do ffmpeg corrige a dimensão
sozinho a partir do SPS in-band e devolve o número certo, escondendo o defeito
do container. Só um leitor puro de container (`mediainfo`) mostra o que está
gravado. Uma primeira passada com `ffprobe` deu a `cam_lateral1` como correta.

### O init é sintético, não "quase certo"

Dentro de cada init, o `stsd` e o SPS do `hvcC`/`avcC` **concordam entre si**. A
mentira é coerente, não um campo solto. E os gens são compartilhados entre
câmeras - `4edbc50d8e70` é byte a byte idêntico em cozinha/frente/fundo/portao/
quintal, `e38cb0530c62` em cozinha/jardim (as duas com flac), `ead03cc7ac5a` em
lateral1/lateral2/porta. O init é função de (codec, áudio) e de mais nada.

### Não existe versão barata do conserto

Testado: remendar só os 8 bytes de `width`/`height` do `stsd`, sem tocar no
`hvcC`.

| Leitor | Resultado |
|---|---|
| `mediainfo` (lê `stsd`) | 640x360 ✓ |
| `ffprobe` (lê o `hvcC`) | 2560x1440 ✗ |

Como quase toda ferramenta é ffmpeg por baixo, o remendo de 8 bytes não resolve
e ainda deixa o init se contradizendo. **É cirurgia de caixa ou nada.**

## Prós

- Todo MP4 exportado das 6 câmeras afetadas declara uma resolução falsa. Se a
  exportação existe para entregar clipe a terceiro, o arquivo entregue mente
  sobre si mesmo.
- **`-c copy` propaga a mentira** - recortar um trecho, que é a coisa mais comum
  de se fazer com um arquivo exportado. Só no HEVC: no AVC o ffmpeg reconstrói o
  `avcC` a partir do in-band e se autocorrige.
- Editor que monta a timeline pelas dimensões do container abre projeto 1440p
  sobre material 360p.
- Efeito colateral bom: o `Gen` voltaria a significar "mesma configuração de
  codec" também em H265, resolvendo a Consequência 2 do documento de causa raiz.

## Contras

- **Nada quebra hoje.** mpv, gstreamer, ffmpeg e o navegador decodificam certo,
  porque `hev1` sinaliza legitimamente parameter sets in-band. Verificado nos
  três primeiros.
- **Não há distorção.** Os 7 casos são 16:9 dos dois lados: a imagem sai
  correta, só o número está errado.
- Reencode sempre produz saída correta.
- A interface do dwnvr já mostra a resolução certa desde o `3943d31`, que lê o
  SPS in-band.
- O custo é cirurgia de caixa de verdade: o SPS real tem tamanho diferente do
  dummy, então é preciso corrigir o tamanho de toda a cadeia acima dele
  (`hvcC` → sample entry → `stsd` → `stbl` → `minf` → `mdia` → `trak` → `moov`).
  Estimativa: ~150 linhas mais testes.
- **O raio de explosão é o pior aspecto.** Errar a aritmética de tamanho
  corrompe o init de toda gravação nova.

## Como implementar, se um dia valer

Corrigir **na gravação**, não na saída - supondo que as gravações antigas sejam
descartáveis, o que foi confirmado em 09/08/2026.

1. Mover o `WriteInit` do caso `moov` (`internal/recorder/recorder.go:325`) para
   o primeiro keyframe. **Custo de latência: zero.** O `maybeRotate` já retorna
   cedo em fragmento que não é keyframe, então o segmentador já espera o
   primeiro keyframe para abrir o primeiro segmento, e os bytes já estão em
   `seg.init`.
2. Trocar os parameter sets do `hvcC`/`avcC` pelos in-band e acertar o `stsd`.
3. O `gen` passa a sair do init corrigido de graça, e vira distinto por câmera.

O dado que torna isso barato: **1 fragmento = 1 frame ≈ 66 ms** (~15 fps), e o
primeiro fragmento de todo segmento traz VPS+SPS+PPS in-band nas 9 câmeras
(24 KB–191 KB). O SPS verdadeiro está sempre à mão no momento em que o init
seria escrito.

Ferramentas que já existem: `fmp4.FindSPS`, `fmp4.SPSSize`, `fmp4.BoxPayload` e
o `walk` interno.

**Mitigação obrigatória do raio de explosão:** reparsear o init reescrito com
`ParseMoov` antes de gravar e cair de volta nos bytes originais se não parsear.
Assim o pior caso vira "igual a hoje" em vez de "arquivo corrompido".

## Vale a pena?

**Não agora.** É um bug de metadado sem vítima funcional: ninguém perde
gravação e nada no disco está inutilizável.

O critério para reabrir é um só: **com que frequência um arquivo exportado é
entregue a alguém fora do dwnvr?** Se exportar é raro e o consumo é pela própria
interface, o SPS in-band já cobre. Se a exportação existe justamente para
entregar clipe a terceiro, aí o defeito é no produto entregue e o `-c copy` é a
aresta mais afiada.

E uma comparação de prioridade que pesa mais que tudo acima: **a `cam_portao`
gravando 640x360 é um problema maior que este metadado inteiro.** Se ela é a
câmera que vê quem chega no portão, a fonte no `go2rtc.yaml` provavelmente
aponta para o substream em vez do canal principal. Ali se perde pixel de
verdade, não um número num cabeçalho, e o conserto é uma linha de YAML.
