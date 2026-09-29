# TODO - o log da cota diz `liberado_mb=0` quando liberou

**Status: proposta, não implementada.**

## O problema

Com a câmera no limite da cota, a retenção roda a cada minuto e apaga só o
excedente daquele minuto: um ou dois segmentos, algumas centenas de KB. O log
arredonda para MB com `freed>>20`, e sai isto, a cada minuto e em cada câmera:

```text
level=INFO msg="cota excedida, evictando" cam=cam_1 usado_mb=20480 cota_mb=20480 liberado_mb=0
```

Visto numa instalação real em 25/09/2026. Lido do jeito que está, parece
que a retenção tenta e não consegue apagar nada. Não é isso: o segmento mais
antigo do `cam_1` estava sumindo normalmente. O número é que engana, e o volume
(uma linha por câmera por minuto) esconde o resto do log.

Está em `internal/retention/retention.go`, no `enforceQuota`.

## A proposta

- Logar em bytes ou KB (`liberado_kb`), ou dizer quantos segmentos saíram.
- Rebaixar para `Debug` o caso normal de regime (câmera cheia apagando o
  excedente do minuto), e deixar em `Info` só a primeira vez que a câmera
  bate na cota e quando a retenção não conseguir liberar o que precisava.
