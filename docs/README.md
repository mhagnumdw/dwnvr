# Documentação do dwnvr

O [README da raiz](../README.md) responde "o que é, com o quê, como rodo".
Aqui ficam as respostas longas.

## Para operar

- [operacao.md](operacao.md) - o dia a dia: onde ficam os arquivos, como ler
  logs, como inspecionar um container sem shell
- [configuracao.md](configuracao.md) - os dois arquivos de configuração, a
  política por câmera, retenção e o custo de cada modo de áudio

## Para entender

- [arquitetura.md](arquitetura.md) - o formato em disco: por que consumir o
  fMP4 do go2rtc, o índice NDJSON, o init identificado por hash
- [resiliencia.md](resiliencia.md) - os dois modos de falha reais: queda de
  energia e go2rtc que emudece sem avisar
- [api.md](api.md) - referência dos endpoints HTTP

## Medições e investigações

Documentos datados, que registram como cada premissa foi verificada. Não são
mantidos atualizados: valem como evidência do que foi medido, quando.

- [fase0-resultados.md](fase0-resultados.md) - os testes de viabilidade que
  validaram gravação e playback antes de existir projeto
- [fase1-resultados.md](fase1-resultados.md) - recorder, índice e retenção
- [fase2-resultados.md](fase2-resultados.md) - API HTTP
- [fase3-resultados.md](fase3-resultados.md) - a interface
- [fase4-resultados.md](fase4-resultados.md) - empacotamento Docker multi-arch
- [audio-flac.md](audio-flac.md) - a medição do modo FLAC
- [go2rtc-h265-parameter-sets.md](go2rtc-h265-parameter-sets.md) - o go2rtc
  grava parameter sets falsos no container H265, e por que isso não quebra a
  reprodução

## Defeitos conhecidos

[TODO/](TODO/) - um arquivo por defeito ou pendência encontrada fora do escopo
da tarefa em que apareceu, para não se perder nem alargar aquela tarefa.

A interface tem documentação própria em [`web/README.md`](../web/README.md).
