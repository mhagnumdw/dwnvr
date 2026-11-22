# Documentação do dwnvr

O [README da raiz](../README.md) responde "o que é, com o quê, como rodo".
Aqui ficam as respostas longas.

## Para operar

- [operations.md](operations.md) - o dia a dia: onde ficam os arquivos, como
  copiar um trecho da gravação, como ler logs, como inspecionar um container
  sem shell
- [configuration.md](configuration.md) - os dois arquivos de configuração, a
  política por câmera, retenção e o custo de cada modo de áudio
- [remote-access.md](remote-access.md) - o dwnvr em `https://` de qualquer
  lugar e instalável como app, pelo Tailscale, sem expor o servidor; dar
  acesso a outras pessoas
- [https-local-network.md](https-local-network.md) - o dwnvr em `https://` só
  na rede de casa, sem o Tailscale: o Caddy na frente e a CA dele em cada
  aparelho

## Para atualizar

- [upgrading.md](upgrading.md) - o guia da atualização para a 1.0.0, em inglês:
  o que renomear antes de subir a versão nova

## Para entender

- [architecture.md](architecture.md) - o formato em disco: por que consumir o
  fMP4 do go2rtc, o índice NDJSON, o init identificado por hash
- [detection.md](detection.md) - a detecção de movimento e de objetos: o fluxo
  de um quadro até a marca, o `dwnvr-detect`, o custo e os limites do modelo
- [resilience.md](resilience.md) - os dois modos de falha reais: queda de
  energia e go2rtc que emudece sem avisar
- [api.md](api.md) - referência dos endpoints HTTP

## Para manter o repositório

- [github.md](github.md) - o que está ligado no GitHub (Dependabot, CodeQL,
  secret scanning, ruleset na `main`), o comando de cada coisa e o que fazer
  com o PR do Dependabot

## Medições e investigações

Documentos datados, que registram como cada premissa foi verificada. Não são
mantidos atualizados: valem como evidência do que foi medido, quando.

- [phase0-results.md](phase0-results.md) - os testes de viabilidade que
  validaram gravação e playback antes de existir projeto
- [phase1-results.md](phase1-results.md) - recorder, índice e retenção
- [phase2-results.md](phase2-results.md) - API HTTP
- [phase3-results.md](phase3-results.md) - a interface
- [phase4-results.md](phase4-results.md) - empacotamento Docker multi-arch
- [audio-flac.md](audio-flac.md) - a medição do modo FLAC
- [go2rtc-h265-parameter-sets.md](go2rtc-h265-parameter-sets.md) - o go2rtc
  grava parameter sets falsos no container H265, e por que isso não quebra a
  reprodução

## Defeitos conhecidos

[TODO/](TODO/) - um arquivo por defeito ou pendência encontrada fora do escopo
da tarefa em que apareceu, para não se perder nem alargar aquela tarefa.

A interface tem documentação própria em [`web/README.md`](../web/README.md).
