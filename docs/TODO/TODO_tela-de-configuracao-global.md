# TODO - tela de configuração global

**Status: não implementado, por decisão.** Herança só nos campos que forem
para a tela.

## Recomendo virar parâmetro na tela

| Parâmetro | Onde está hoje | Motivo |
|---|---|---|
| `storage.minFreeMB` | `dwnvr.yaml` | É do disco, global por natureza. A retenção relê o valor a cada passada (1 min), então vale sem reiniciar, e o Diagnóstico já mostra "mínimo livre". Precisa de teto: um valor perto do tamanho do disco apaga as gravações de todas as câmeras |
| `defaults.maxDays` | `dwnvr.yaml` | Guardar no máximo N dias é uma escolha que vale para todas as câmeras, e o campo nem aparece no formulário de câmera. A retenção resolve as câmeras a cada passada, então também vale sem reiniciar. Só vale com herança (ver abaixo) |
| `defaults.detect` | `dwnvr.yaml` | Liga ou desliga a marcação de movimento de todas as câmeras num lugar só. Mudar a detecção não reconecta a câmera. Só vale com herança |
| `defaults.detectSensibilidade` | `dwnvr.yaml` | O mesmo nível em todas as câmeras é o caso comum, e hoje ele exige editar câmera por câmera: levar dez câmeras a outro nível custa dez edições. Mudar não reconecta a câmera. Só vale com herança |
| `DETECT_THREADS` | env do `dwnvr-detect` | É o único ajuste de custo x espera do detector de objetos, é global (um container atende todas as câmeras) e depende do hardware: num Orange Pi Zero 3, de 1 para 2 threads a olhada cai de ~6,5 s para ~3,6 s, por ~10% a mais de CPU. O Diagnóstico mostra o efeito em um ou dois minutos ("analisar leva X s", das últimas 20 olhadas). Mora em outro container, ver a seção própria abaixo |

## Não recomendo

| Parâmetro | Onde está hoje | Motivo |
|---|---|---|
| `server.listen` | `dwnvr.yaml` | Amarrado à porta publicada no compose. Um erro deixa a própria tela inacessível, sem tela para desfazer |
| `go2rtc.url`, `go2rtc.username`, `go2rtc.password` | `dwnvr.yaml` | Amarrados ao nome do serviço no compose. Um erro para a gravação de todas as câmeras |
| `storage.root` | `dwnvr.yaml` | Amarrado ao volume do compose. Mudar deixa as gravações existentes para trás, fora da tela |
| `server.username`, `server.password` | `dwnvr.yaml` | Trocar senha é uma feature com desenho próprio, não um campo. Hoje trocar a senha não derruba sessão aberta: o cookie leva só a validade e a assinatura com o `.session-secret`, e vale até 30 dias depois do login. A tela teria que girar o segredo, pedir a senha atual e impedir que alguém desligue a autenticação por ela |
| `defaults.quotaMB` | `dwnvr.yaml` | A cota certa depende da taxa de cada câmera, e o formulário de câmera já a traduz em "≈ N dias". Um número único dá dias muito diferentes em cada câmera |
| `defaults.audio` | `dwnvr.yaml` | Depende de a câmera ter trilha de áudio. O formulário bloqueia flac e aac quando a câmera não entrega áudio, e um padrão global passaria por cima disso |
| `defaults.segmentSeconds` | `dwnvr.yaml` | Ajuste técnico, não escolha de uso: troca o teto do que se perde numa queda de energia pela quantidade de arquivos. Mudar reconecta a câmera |
| `defaults.stallSeconds` | `dwnvr.yaml` | Depende do enlace de cada câmera (Wi-Fi ruim precisa de mais folga que cabo), e mudar reconecta a câmera. Nem o formulário de câmera o mostra |
| `TZ` | env do container do dwnvr | É do container, não da aplicação. Mudar desloca a virada de dia das gravações novas em relação às antigas |
| constantes internas (`sessionTTL`, `maxExportSpan`, `gapTolerance`, backoff de reconexão, intervalo da retenção) | código | Ninguém pediu, e algumas estão amarradas: o `gapTolerance` precisa ser igual ao `LacunaMs` da detecção, senão os limiares medidos deixam de valer |
| `defaults.detectMecanismo` | `dwnvr.yaml` | O `periodico` existe como régua de comparação e para quem quer um comportamento sem surpresa numa câmera específica, não como padrão de todas |
| `detector.url` | `dwnvr.yaml` | Ligação entre containers, amarrada ao nome do serviço no compose. Hoje é lida só na subida: é dela que a fila nasce ou não |
| `DETECT_MODELO` | env do `dwnvr-detect` | Outro `.onnx` entra por volume no compose, e a tela não monta volume |
| `DETECT_PORTA` | env do `dwnvr-detect` | Ligação entre containers, e tem que bater com a `detector.url` |
| números do `internal/detect/parametros.go`: corte 0,40, piso 0,20, IoU 0,7, 5 faltas, janela de 3 s, 2 pedaços por câmera, timeout de 60 s, teto de 4 MB do GOP, parâmetros do score e `LimiarKleinbergP` | código | É medição, não preferência. Expor seria decisão técnica no colo do usuário, e qualquer mudança faz a medição deixar de valer. Há acoplamento que não se vê: o `LimiarKleinbergP` só corresponde aos cinco níveis enquanto os parâmetros do score não mudam. O corte de confiança é o que outros NVRs expõem, e a resposta é a mesma |
| famílias que viram marca (`FamiliasPadrao`) | código | Desligar `animal` não economiza CPU, porque a olhada acontece igual, e a marca não seria gravada: some para sempre. Se um dia incomodar, o caminho é um filtro na timeline |

## O critério

Cada parâmetro das tabelas passou por quatro perguntas, e parâmetro novo passa
pelas mesmas:

1. **Errar o valor derruba a gravação, trava a tela ou obriga a mexer no compose
   junto?** Fica no arquivo.
2. **É escolha de uso, que se muda olhando o efeito?** Entra, de preferência com
   o efeito visível no Diagnóstico.
3. **É número medido do `parametros.go`?** Não entra.
4. **Vale sem reiniciar?** Tela que termina em `docker compose restart` não
   economiza nada.

## A herança, e por que ela vem primeiro

Herança é a câmera não guardar o valor de um campo e usar o valor global
enquanto não tiver um próprio. Com a sensibilidade global em 4, a câmera em
"usar o padrão" fica sem o campo no `cameras.json`, usa 4 e muda junto se o
global mudar. A câmera com valor próprio 3 continua no 3.

O código já faz isso: o `config.Resolve` preenche o campo vazio com o valor do
`defaults`. O problema é que a tela nunca deixa o campo vazio:

- `novo()`, no `web/src/routes/Cameras.svelte`, cria a câmera com
  `quotaMB: 10240`, `segmentSeconds: 30` e `audio: 'none'` escritos no código,
  e os três campos da detecção copiados do `padrao` da API;
- `editar()` parte da câmera que o `GET /api/cameras` já devolve resolvida
  (`handleCameras`, em `internal/api/server.go`), e salvar grava tudo, inclusive
  `stallSeconds` e os três campos da detecção.

Salva pela tela, a câmera não acompanha mais o global. O que o
`docs/configuracao.md` diz ("Os valores de `defaults` no `dwnvr.yaml` valem para
qualquer câmera que não defina o campo") é verdade no código e quase nunca na
prática: toda câmera que passou pela tela ignora uma mudança de
`defaults.detectSensibilidade` no `dwnvr.yaml`.

Sem herança, a tela fica com dois campos (`minFreeMB` e `DETECT_THREADS`), e uma
seção de padrões enganaria quem usa.

## O `DETECT_THREADS` mora em outro container

- **O dwnvr não muda env de outro container.** O caminho que respeita o contrato
  atual é o mesmo do `piso`: o dwnvr manda o valor a cada pedido
  (`POST /detect?piso=0.20&threads=2`), e a env vale só quando o pedido não
  disser nada. O sidecar continua sem estado.
- **Trocar o número exige reiniciar o sidecar.** O onnxruntime fixa as threads
  quando a sessão é criada (`intra_op_num_threads`, em
  `dwnvr-detect/servidor.py`), e a memória de um modelo descartado não volta ao
  sistema dentro do mesmo processo, num container com teto de 400 MB. O seguro é
  o sidecar se reiniciar com o número novo: fica alguns segundos sem olhar, e a
  gravação não é afetada. A olhada que falhar nesse meio cai na pausa de 30 s da
  fila (`PausaDepoisDeFalhaMs`).
- **O limite é o `cpus` do compose, que a tela não muda.** Acima dele, threads
  brigando por núcleo são piores que menos threads. O `/health` teria que
  informar o limite de CPU do container, e a tela limitaria o campo a ele. Com
  o `cpus: "2"` do compose padrão, o campo teria só 1 e 2. Ver também
  [`TODO_detect-threads-auto.md`](TODO_detect-threads-auto.md), que resolve o
  mesmo problema sem tela.

## Onde os valores ficam gravados

O `dwnvr.yaml` nunca é reescrito pela aplicação, de propósito: reescrever um
YAML apaga os comentários de quem o escreveu. O que for para a tela precisa de
um terceiro arquivo, gravado por ela ao lado do `cameras.json`, e tem que sair
do `dwnvr.yaml`. Se ficar nos dois, a tela pode mostrar um valor que o yaml
contradiz.

Na primeira subida depois da mudança, o valor que a instalação já tem no yaml
vira o valor inicial.

## Como implementar, quando valer

1. O arquivo da tela, com gravação atômica igual à do `SaveCameras` (arquivo
   temporário e rename). Precedência: arquivo da tela, depois `dwnvr.yaml`,
   depois o código.
2. A herança nos campos da tela. O `maxDays` precisa distinguir "usar o padrão"
   de "sem limite", que hoje são o mesmo zero: vira ponteiro, como o `Detect`.
3. O `GET /api/cameras` passa a devolver o valor próprio de cada câmera separado
   do resolvido. O formulário mostra "padrão (N)" ou o valor próprio, e manda o
   campo vazio quando a escolha for o padrão.
4. Rota para ler e gravar os ajustes. Ao gravar, o `Manager` reaplica (`Set`) as
   câmeras que herdam, e a retenção já relê a cada passada. O `Config` hoje é
   lido sem trava porque nunca muda depois da subida; passando a mudar, precisa
   de uma. Mudar a detecção não reconecta: o recorder já troca o mecanismo no
   quadro seguinte (`detectPedido`).
5. O teto do `minFreeMB`, calculado sobre o tamanho do disco.
6. As câmeras que já existem continuam com valor próprio. Não converter
   sozinho: não dá para saber o que foi escolhido e o que a tela gravou só
   porque sempre grava.
7. Validar na API e também na leitura do arquivo, que é outra porta de entrada
   (ver [`TODO_limites-numericos-so-valem-na-api.md`](TODO_limites-numericos-so-valem-na-api.md)).
8. O `POST /detect` aceita `threads`, o sidecar se reinicia quando o número
   muda, e o `/health` informa o limite de CPU do container.

O resto (campo novo no `config.go`, rota nova, tela nova, `make web`) está na
tabela de repercussões do `AGENTS.md`.

## Vale a pena?

**Não agora.** A herança é a parte cara, porque mexe no formulário de câmera,
na API de câmeras e no significado do `cameras.json`, e sem ela a tela não se
paga.

O critério para reabrir: mudar a detecção de todas as câmeras virar rotina, ou
o dwnvr ir para o hardware de outra pessoa, onde ninguém mediu o
`DETECT_THREADS` certo.
