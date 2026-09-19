# TODO - o dwnvr-detect travado não é reiniciado

**Status: risco conhecido, nunca observado.**

## O problema

O compose sobe o `dwnvr-detect` com `restart: unless-stopped`, que só age
quando o processo **termina**. Se o `servidor.py` travar sem morrer (inferência
presa, deadlock no lock `vez`), o processo continua vivo e nada o reinicia.

O `HEALTHCHECK` do Dockerfile não resolve: ele consulta o `/health`, que fica
**fora** do lock da detecção de propósito, então responde normalmente mesmo com
uma olhada presa. E, mesmo que o container ficasse `unhealthy`, o Docker sozinho
não faz nada com esse estado.

## O efeito

A fila do dwnvr aguenta bem: cada tentativa espera o timeout de 60 s
(`PrazoDaOlhadaMs`), conta como falha e pausa 30 s (`PausaDepoisDeFalhaMs`). A
gravação e as marcas de movimento seguem normais. Mas **nenhuma marca de objeto
aparece até alguém reiniciar o container à mão**, e os únicos avisos são uma
linha de log ("detector de objetos fora do ar") e as falhas contando no funil
do Diagnóstico.

## Saídas possíveis

1. **Watchdog no próprio `servidor.py`**: se uma detecção passar de um prazo
   (ex.: 50 s, abaixo dos 60 s do dwnvr), o processo sai com erro, e o
   `restart: unless-stopped` o levanta. Não exige nada novo no compose.
2. **O `/health` passar a refletir a olhada presa** + um container `autoheal`
   que reinicia quem estiver `unhealthy`. É mais uma peça para manter.

A 1 parece suficiente.
