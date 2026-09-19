# TODO - `DETECT_THREADS=auto` como padrão

**Status: proposta, não implementada.**

## O problema

O `DETECT_THREADS` é fixo. O `servidor.py` usa 1 quando a variável não vem, e
o `docker-compose.yml` põe 2, junto com `cpus: "2"`. Os dois números foram
medidos num Orange Pi Zero 3: com 2 threads a olhada cai de ~6,5 s para
~3,6 s, por ~10% a mais de CPU no total.

Em outro hardware ninguém mediu nada. Numa placa de 2 núcleos, o 2 do compose
toma a máquina inteira de quem grava; numa máquina folgada, ele desperdiça
núcleo parado. O valor certo tem que ser descoberto e posto à mão.

## A proposta

`DETECT_THREADS=auto`, e ele passa a ser o padrão:

| Situação | `auto` vira | Por quê |
|---|---|---|
| Container com limite de CPU (`cpus:` no compose) | **o limite**, arredondado para baixo, mínimo 1 | quem montou o compose já decidiu quanto dar ao detector de objetos; mais threads que isso só pioram |
| Sem limite | **metade dos núcleos visíveis**, mínimo 1 | a outra metade fica para o dwnvr e o go2rtc, que não podem perder segmento |
| Número explícito | o número | quem mediu manda |

**A armadilha: nunca `os.cpu_count()`.** Ele devolve os núcleos da MÁQUINA, e
o onnxruntime já ignora o limite do container - é por isso que o
`servidor.py` fixa o `intra_op_num_threads`. Numa máquina de 8 núcleos com
`cpus: "2"`, daria 4 threads brigando por 2 núcleos, o que é pior que 2.

- **O limite** vem do `/sys/fs/cgroup/cpu.max` (cgroup v2): `quota período`,
  ou `max` quando não há limite. Threads = `quota // período`, mínimo 1.
- **Os núcleos visíveis** vêm de `os.sched_getaffinity(0)`, que respeita o
  `cpuset`. A imagem é Python 3.12, sem `os.process_cpu_count()`.

## O que conferir

- **Num Orange Pi Zero 3 com o compose padrão deve dar 2** pelas duas regras
  (`cpus: "2"`, e 4 núcleos / 2). Com isso o `DETECT_THREADS=2` sai do
  `docker-compose.yml`.
- O número escolhido vai para a linha de subida do log, e o `/health` já
  informa `threads`.
- Numa placa de 2 núcleos sem limite, deve dar 1.

## Pergunta aberta

Só 1 e 2 threads foram medidos. Numa máquina de 16 núcleos sem limite, o
`auto` daria 8, e ninguém sabe se o `rfdetr-n` a 512x288 ganha algo acima de 2
ou 4. Um teto (ex.: 4) seria prudência sem medição; a proposta é ficar sem
teto, porque o `cpus:` do compose já serve de freio, e medir quando houver
máquina maior à mão.
