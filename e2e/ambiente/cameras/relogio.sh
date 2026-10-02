#!/usr/bin/env bash
# Câmera "relógio" dos testes ponta a ponta: a hora de parede queimada em cada
# quadro, em texto (para quem assiste ao vídeo do teste) e numa faixa de 20
# blocos no topo (para o teste ler o pixel). O bloco i é branco quando o bit i
# do instante em segundos (epoch) é 1: a faixa guarda o epoch módulo 2^20, que
# só se repete a cada ~12 dias. A leitura está em e2e/apoio/relogio.ts.
#
# O go2rtc a roda com `exec:/cameras/relogio.sh {output}`, dentro da imagem
# dele: o ffmpeg, o bash e a fonte Droid são os de lá.
#
# O keyframe a cada 2 s (-g 30 a 15 fps) deixa o segmento de 10 s fechar
# perto dos 10 s, e o seek cair perto de onde se tocou.
set -euo pipefail

saida="$1"
# O instante de partida da faixa, com os microssegundos: o `date +%s` trunca
# o segundo, e a faixa andaria até 1 s atrás do relógio de parede. Ela ainda
# fica um pouco atrás, o tempo que o ffmpeg leva até entregar o primeiro
# quadro (o realtime ancora no primeiro quadro, e não no e0); o teste conta
# com isso. A vírgula troca porque o EPOCHREALTIME segue o separador decimal
# do locale.
e0=${EPOCHREALTIME/,/.}
fonte=/usr/share/fonts/droid/DroidSansMono.ttf

# As aspas simples são de propósito: é o drawtext, e não o bash, que expande o
# %{localtime}, e os dois-pontos precisam chegar escapados a ele.
# shellcheck disable=SC2016
hora='%{localtime\:%H\\\:%M\\\:%S}'

grafo="color=c=black:s=640x24:r=15,format=yuv420p,geq=lum='255*mod(floor(($e0+T)/pow(2,floor(X/32))),2)':cb=128:cr=128[faixa];testsrc2=size=640x360:rate=15,format=yuv420p[fundo];[fundo][faixa]overlay=0:0,drawtext=fontfile=$fonte:text='$hora':x=10:y=40:fontsize=40:fontcolor=white:box=1:boxcolor=black@0.7,realtime[v]"

exec ffmpeg -hide_banner -loglevel error -filter_complex "$grafo" -map '[v]' \
  -c:v libx264 -g 30 -profile:v high -preset superfast -tune zerolatency -pix_fmt yuv420p \
  -rtsp_transport tcp -f rtsp "$saida"
