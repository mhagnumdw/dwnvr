# Resiliência: como o dwnvr sobrevive a quedas e a streams mudos

Um NVR passa 24 horas por dia esperando que nada dê errado, e o que dá errado
raramente avisa. Este documento descreve os dois modos de falha que o dwnvr
enfrenta de verdade - o corte de energia e o go2rtc que emudece - e a defesa
que existe contra cada um.

- [Recuperação de queda](#recuperação-de-queda)
- [Quando o go2rtc emudece](#quando-o-go2rtc-emudece)

## Recuperação de queda

O índice é escrito **depois** que o segmento é fechado. Uma queda entre as duas
coisas deixa um arquivo órfão, que a reconciliação do boot reincorpora sondando
o arquivo. A ordem inversa deixaria o índice apontando para algo que nunca
existiu.

Só o dia mais recente é reconciliado: é onde mora o estrago de uma queda, e
conferir só ele evita varrer centenas de milhares de arquivos a cada boot.

Verificado no Orange Pi Zero 3: `kill -9` deixou 1 órfão de 786.432 bytes (a
cauda bufferizada se perdeu); o reinício o reincorporou com a duração correta.

## Quando o go2rtc emudece

A falha mais perigosa de um NVR não é parar de gravar: é parar de gravar **sem
avisar**. E o go2rtc produz exatamente isso.

Os produtores RTSP dele rodam sobre UDP. Quando o fluxo da câmera para, não há
erro de socket nenhum - o go2rtc simplesmente deixa de escrever, com a resposta
HTTP aberta. Do lado de cá, `Read` bloqueia para sempre: sem erro, sem EOF, sem
log, e nada aciona a reconexão.

Aconteceu em 09/08/2026: as 9 câmeras pararam às 08:18. Quatro voltaram sozinhas
2h30 depois, quando o go2rtc recriou o produtor por conta própria. Cinco ficaram
**3h38 paradas** reportando `connected: true` e `reconnects: 0`. Enquanto isso o
log acumulava 380 avisos - todos sobre 404 de miniatura, nenhum sobre as câmeras.

Por isso todo stream aberto carrega um limiar de inatividade (`stallSeconds`,
15s por padrão): passou disso sem receber um byte, a conexão cai e o backoff que
já existe reconecta.

Fechar a conexão é também o que **recupera**. Como o dwnvr é o único consumidor
do stream, sair faz o go2rtc derrubar o produtor morto, e a reabertura força uma
sessão RTSP nova. Medido numa câmera parada havia 3h37: voltou a gravar em menos
de 1 segundo.

A primeira leitura ganha o dobro do prazo, porque abrir o stream faz o go2rtc
estabelecer a sessão RTSP com a câmera - legitimamente mais lento que entregar o
próximo fragmento de um stream que já está correndo.

O limiar é ajustável por câmera: veja `stallSeconds` em
[configuracao.md](configuracao.md). A implementação está em
[`internal/go2rtc/client.go`](../internal/go2rtc/client.go).
