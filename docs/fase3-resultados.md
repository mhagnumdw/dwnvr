# Fase 3 — interface

SPA em Svelte 5 + Vite, embutida no binário. Validada contra o Orange Pi Zero 3
com as 9 câmeras gravando, 08/08/2026.

## Tamanho

| | cru | gzip |
|---|---|---|
| JS (inclui o player de live do go2rtc) | 88,2 kB | **32,6 kB** |
| CSS | 9,9 kB | 2,7 kB |
| HTML | 0,5 kB | 0,3 kB |

O binário ARM64 com tudo embutido: 6,8 MB. Servir o bundle do Pi levou 17 ms.

Escrever o player MSE em vez de usar hls.js é o que segura esse número: só a
biblioteca já custaria ~110 kB gzip, mais que triplicando o aplicativo inteiro.

## As quatro telas

**Ao vivo** — grade de 1 a 3 colunas (sempre 1 no celular), seleção persistida
em localStorage. Usa o `<video-stream>` do próprio go2rtc, que negocia
WebRTC/MSE sozinho. Confirmado no Pi: os dois tiles conectaram em modo MSE
através do proxy do dwnvr, decodificando 1920x1080.

**Gravações** — timeline de 24h em canvas, tira de miniaturas e player MSE.
Verificado: o relógio da interface mostrou 19:58:52 no mesmo instante em que o
OSD gravado pela câmera mostrava 19:58:52, o que valida o mapeamento de relógio
de parede ponta a ponta.

**Câmeras** — cadastro a partir dos streams que o go2rtc já serve, com o modo de
áudio desabilitado nas câmeras que não entregam trilha de áudio. A cota aparece
sempre acompanhada da retenção estimada na taxa medida.

**Diagnóstico** — medidor de disco separando o que é do dwnvr do que é de
terceiros, totais (9 conectadas, 4,52 Mbps, 45,5 GB/dia) e uma lista de avisos
que explica problemas antes de virarem mistério.

## Miniaturas sem custo no Pi

Cada miniatura é o init mais o primeiro fragmento do segmento — um MP4 de um
frame recortado do que já está gravado. Quem decodifica é o navegador, via
WebCodecs, com `<video>` oculto como alternativa.

O Pi só recorta bytes. Confirmado na tela: a tira mostra frames reais a cada
~30 s de gravação.

## Cadastro aplica em quente

Cadastrar uma câmera e ter de reiniciar o serviço para ela gravar seria um bom
jeito de perder gravação sem perceber. O gerenciador ganhou `Set` e `Remove`,
cada recorder com seu próprio cancelamento.

Duas decisões dentro disso:

- **Reiniciar só quando muda o que o recorder usa.** Trocar o nome de exibição
  ou a cota não derruba a conexão; mudar áudio ou duração de segmento sim.
- **A retenção passou a ler a lista viva** em vez de uma cópia feita na subida.
  Uma cota alterada pela tela precisa valer na passada seguinte.

E remover uma câmera **não apaga as gravações**: apagar horas de vídeo como
efeito colateral de um clique seria destrutivo demais para ser implícito. A API
responde com o caminho onde elas ficaram.

## Responsividade

Mobile-first: as regras base valem para o celular e as media queries acrescentam
o que a tela grande permite.

- navegação inferior no celular (alcance do polegar), superior no desktop
- alvos de toque de no mínimo 44 px
- grade ao vivo travada em uma coluna abaixo de 640 px — dois vídeos lado a lado
  num telefone não mostram nada útil de nenhum dos dois
- formulário de cadastro sobe como folha a partir da base, respeitando
  `env(safe-area-inset-bottom)`
- timeline com Pointer Events: arrastar navega, pinçar dá zoom, e
  `touch-action: none` impede a página de rolar junto

Medido a 500 px de largura: **sem rolagem horizontal**, navegação inferior
ativa, superior oculta.

## Três correções encontradas olhando a tela pronta

**O relógio ficava em `--:--:--` com o vídeo já visível.** `currentMs` só era
atualizado em `timeupdate`, que não dispara quando o autoplay é bloqueado. Passou
a ouvir `seeked` e `loadeddata` e a refletir a posição logo após o seek.

**A retenção estimada aparecia como "0h".** Cotas pequenas dão frações de hora, e
arredondar tudo para horas não informava nada. Agora mostra minutos abaixo de
uma hora.

**O live mostrava barra de progresso.** O componente do go2rtc cria o `<video>`
com `controls=true`, o que numa grade ao vivo exibe uma linha do tempo que não
existe — e ainda aparecia em umas câmeras e não em outras, conforme o modo
negociado. Desligado, com duplo clique para tela cheia no lugar.

## Nota sobre o build versionado

`go:embed` exige que os arquivos existam em tempo de compilação, então
`internal/api/dist` é versionado. Isso faz `go build ./...` funcionar num clone
limpo sem Node instalado — o que importa quando o alvo é um dispositivo onde
ninguém quer instalar toolchain de frontend.

Ao alterar `web/`, é preciso rodar `npm run build` antes de commitar.
