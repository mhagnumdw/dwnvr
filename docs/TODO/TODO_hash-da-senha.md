# TODO - hash da senha do login

Registrado em 07/10/2026, com a decisão de fazer numa atividade própria. Aqui
fica o estudo, para não refazer: o algoritmo, o custo medido, o desenho e o que
ainda falta decidir.

**Status: não começado.** Os números foram medidos em 07/10/2026; o desenho é
proposta.

## Por quê

Hoje o `server.password` fica no `dwnvr.yaml` do jeito que foi escrito, e o
login o compara, em tempo constante, com o que foi digitado (`handleLogin`, em
`internal/api/auth.go`). O `chmod 600` do passo 2 do
[Instalar de verdade](../../README.md#instalar-de-verdade) já impede que outros
usuários e serviços da máquina leiam o arquivo.

O hash acrescenta duas coisas:

- **Uma cópia do arquivo não entrega a senha.** Um backup, o yaml colado num
  pedido de ajuda, um print: a senha pode ser a mesma do e-mail.
- **Só o yaml não basta para entrar.** Sem o `.session-secret`, o hash não
  serve para o login nem para fabricar o cookie.

E não resolve:

- **Quem copia a pasta inteira** leva o `.session-secret` junto e fabrica o
  cookie sem saber a senha, porque a chave sai do segredo e do valor guardado
  no yaml (`deriveSessionKey`, em `internal/api/auth.go`).
- **Quem tem root na máquina.**
- **Tentar senhas pela tela de login.** Isso é o
  [limite de tentativas](TODO_limite-de-tentativas-no-login.md). O hash deixa
  cada tentativa mais cara, mas não limita.

Só o `server.password` pode virar hash. O `go2rtc.password` é a credencial que
o dwnvr apresenta ao go2rtc, e ele precisa dela como foi escrita.

É o mesmo esquema do `htpasswd`, usado no nginx e no Apache, e do
`caddy hash-password`: uma ferramenta gera o hash, e a configuração guarda o
hash.

## O algoritmo

### O que a OWASP recomenda

A OWASP (Open Worldwide Application Security Project) é uma fundação sem fins
lucrativos, de 2001, que publica material aberto de segurança de aplicação. A
fonte é o
[Password Storage Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html),
conferido em 07/10/2026. A ordem de preferência:

1. **Argon2id**: no mínimo 19 MiB de memória, 2 iterações e paralelismo 1. A
   própria página lista configurações de mesma defesa, que trocam memória por
   CPU: 46 MiB e 1 iteração, 12 MiB e 3, 9 MiB e 4, 7 MiB e 5.
2. **scrypt**: N=2^17 (128 MiB), r=8, p=1, com equivalentes de menos memória.
3. **bcrypt**: só em sistema legado, custo 10 ou mais, senha de até 72 bytes.
4. **PBKDF2**: quando se exige FIPS-140, com 600.000 iterações de HMAC-SHA-256.

### O que o Go oferece

- **A biblioteca padrão** (Go 1.27.1) tem o `crypto/pbkdf2`, desde o Go 1.24.
  Não tem Argon2, bcrypt nem scrypt.
- **O `golang.org/x/crypto`** (v0.57.0) traz o bcrypt pronto: gera, confere e
  tem formato próprio (`$2a$10$...`). O pacote `argon2` exporta só `IDKey`,
  `Key` e `Version`: o salt, o texto guardado e a comparação ficam por nossa
  conta, umas 30 linhas, as mesmas que o PBKDF2 da biblioteca padrão pede.
- **O Argon2 do Go só tem versão acelerada (SIMD) para amd64.** No arm64 roda
  o código genérico, e é por isso que ele perde vantagem no Orange Pi (ver
  [Custo de um login](#custo-de-um-login)).

### Tamanho no binário

O dwnvr compilado com as flags do Dockerfile, com e sem cada opção (o método
está em [Como medir de novo](#como-medir-de-novo)):

| Opção | Vem de | Código a mais (arm64 / amd64) | Arquivo a mais (arm64 / amd64) |
| --- | --- | --- | --- |
| PBKDF2 | biblioteca padrão | 7 / 8 KB | 0 / 8 KB |
| bcrypt | `x/crypto` | 24 / 24 KB | 0 / 24 KB |
| Argon2id | `x/crypto`, e `x/sys` no amd64 | 24 / 82 KB | 0 / 80 KB |
| `x/term`, para digitar a senha sem mostrar | `x/term` e `x/sys` | 7 / 8 KB | 0 / 8 KB |

- O binário tinha 8,2 MB no arm64 e 8,8 MB no amd64: o pior caso é +0,9%.
- No arm64 o arquivo não mudou em nenhum caso, porque o linker alinha os
  segmentos em 64 KB e o acréscimo coube na sobra.
- O `x/crypto` inteiro tem 2,3 MB de download, mas no binário só entra o
  pacote importado.

O custo que pesa não é o tamanho. O README diz que "a única dependência Go do
projeto é `go.yaml.in/yaml/v3`" e que "essa ausência é o projeto". O bcrypt e o
Argon2id quebram essa frase e trazem um PR do Dependabot a cada release do
`x/crypto`.

### Custo de um login

Medido em 07/10/2026 com o Go 1.27.1. Mediana de 7 vezes, depois de uma de
aquecimento:

| Algoritmo | Notebook (i7-13700H) | Orange Pi Zero 3 | Memória alocada por login |
| --- | --- | --- | --- |
| PBKDF2-SHA256, 600.000 iterações | 87 ms | 0,77 s | 0,8 KiB |
| PBKDF2-SHA256, 100.000 iterações | 15 ms | 0,12 s | 0,8 KiB |
| bcrypt, custo 10 | 55 ms | 0,39 s | 5 KiB |
| Argon2id, 19 MiB, 2 iterações | 18 ms | 0,35 s | 19 MiB |
| Argon2id, 7 MiB, 5 iterações | 16 ms | 0,32 s | 7 MiB |
| HMAC do cookie, a cada pedido (o de hoje) | 1 µs | 4 µs | 0,5 KiB |

- **O Orange Pi** tem 4 núcleos Cortex-A53 a 1,4 GHz, com as instruções de
  SHA-256 em hardware (`sha2` em `/proc/cpuinfo`), que o Go usa. A medição
  rodou com a gravação e o detector de objetos ligados (o detector em ~196% de
  CPU, load perto de 3), em prioridade baixa. O tempo de relógio ficou igual
  ao de CPU: a disputa não inflou os números.
- **O PBKDF2 escala em linha reta**: ~1,3 ms a cada mil iterações no Orange Pi.
  Serve para recalibrar sem medir de novo.
- **Contra o notebook**, o Orange Pi foi 7 a 9 vezes mais lento no PBKDF2 e no
  bcrypt, e 19 a 20 vezes no Argon2id. No notebook o Argon2id é 5 vezes mais
  rápido que o PBKDF2 de 600.000; no Orange Pi, só 2.
- **Memória.** O dwnvr usa ~25 MiB próprios no Orange Pi, fora o page cache, e
  o compose limita o container a 128 MiB. O Argon2id de 19 MiB levou o pico do
  processo de teste de 10,5 para 41 MiB, provavelmente porque o bloco da vez
  anterior ainda não tinha sido recolhido pelo GC; o de 7 MiB, para 17 MiB. O
  PBKDF2 e o bcrypt não mexem no pico.
- **O login é raro.** A sessão vale 30 dias e se renova com o uso: cada
  aparelho faz login no primeiro acesso, depois de uma troca de senha ou depois
  de um mês sem abrir.

### A escolha: PBKDF2-HMAC-SHA256, 600.000 iterações

- **Nenhuma dependência nova.** A frase do README continua verdadeira.
- **0,77 s num login raro**, com os números da OWASP como estão.
- **Quase nada de memória**, longe do limite de 128 MiB do container.

É o quarto da lista da OWASP, o mais fraco contra GPU. Mas a GPU só entra em
jogo para quem tem o hash na mão, ou seja, uma cópia do `dwnvr.yaml`: aí a
pessoa testa senhas no próprio computador, sem passar pelo dwnvr. Pela tela de
login, cada tentativa é calculada pelo próprio dwnvr, e o algoritmo tanto faz.
E, com o yaml vazado, a diferença entre os quatro só decide numa senha mediana:
senha forte não cai em nenhum, senha fraca cai em todos.

**A alternativa** é o Argon2id de 7 MiB e 5 iterações: 0,32 s no Orange Pi e
mais resistente a GPU, ao custo do `x/crypto` e de 7 MiB por login. Ele exige
a fila do [desenho](#o-desenho-proposto) (item 7) de qualquer jeito: cada
tentativa simultânea aloca a sua memória, e, com o de 19 MiB, umas cinco
tentativas ao mesmo tempo já passam dos 128 MiB do container. Aí o kernel mata
o dwnvr, e a gravação para até ele voltar.

**Reabrir a escolha** se o `x/crypto` entrar no projeto por outro motivo: aí o
Argon2id deixa de custar a dependência.

## O desenho proposto

1. **Um contrato, o que em Java seria uma interface**, com "gerar o valor
   guardado a partir da senha" e "conferir a senha digitada contra o valor
   guardado". E um mapa do prefixo para a implementação: `pbkdf2-sha256` hoje,
   `argon2id` se um dia vier, trocando uma linha.
2. **O valor guardado** no formato de texto do PHC, o mesmo estilo do
   `$argon2id$v=19$m=...` de outras ferramentas:
   `$pbkdf2-sha256$i=600000$<salt>$<hash>`, com o salt e o hash em base64 sem
   padding. As iterações vão no texto: subir o padrão depois não invalida os
   hashes que já existem.
3. **O texto puro continua valendo.** Um valor sem o prefixo é comparado como
   hoje. Não é mudança incompatível, e quem não gerar o hash não paga nada.
4. **Os números num `parametros.go`**: as iterações (600.000), o salt (16
   bytes do `crypto/rand`), o tamanho do hash (32 bytes) e a faixa de
   iterações aceita na leitura.
5. **O login calcula o hash sempre**, mesmo com o usuário errado, para manter
   o que o `handleLogin` já garante: comparar sempre os dois campos, para não
   vazar por tempo se o usuário existe. A comparação final é em tempo constante
   (`subtle.ConstantTimeCompare`).
6. **A sessão continua amarrada à credencial.** O `deriveSessionKey` usa o
   valor guardado, que passa a ser o hash: trocar a senha gera outro hash e
   derruba as sessões, como hoje. Gerar outro hash da mesma senha também
   derruba, porque o salt muda.
7. **Uma verificação por vez**, até o
   [limite de tentativas](TODO_limite-de-tentativas-no-login.md) existir:
   cada uma custa 0,77 s de um núcleo no Orange Pi, e o login não limita
   tentativas.
8. **A flag `-hash-password`** no `cmd/dwnvr/main.go`, como a `-healthcheck`:
   pede a senha duas vezes, sem mostrar na tela, imprime o valor para o
   `server.password` e sai. O dwnvr nunca escreve no `dwnvr.yaml`, então quem
   gera o hash é quem instala, com o próprio binário:

   ```sh
   # Sem nada no ar, inclusive antes de subir a primeira vez
   docker run --rm -it ghcr.io/mhagnumdw/dwnvr:<versão> -hash-password

   # Com o dwnvr no ar
   docker exec -it dwnvr /dwnvr -hash-password
   ```

   A biblioteca padrão não lê senha sem mostrar na tela: ou entra o `x/term`
   (+8 KB, e as dependências `x/term` e `x/sys`), ou umas 15 linhas de syscall
   no terminal, que mantêm o projeto sem dependência nova.
9. **No YAML**, o valor começa com `$`. Sem aspas ele é válido, e o dwnvr não
   expande variáveis no arquivo; mesmo assim, a doc mostra entre aspas simples.
10. **A tela de login já aguenta a espera**: mostra "entrando…" com o botão
    desabilitado enquanto o pedido não volta (`web/src/routes/Login.svelte`).

## Decidir antes de implementar

- **Hash malformado no boot** (base64 inválido, iterações fora da faixa):
  recusar a subida, o que para a gravação junto, ou subir gravando e recusar
  todo login, com erro no log. Recomendo o segundo: a gravação não depende do
  login.
- **Ler a senha da entrada padrão** quando ela não é um terminal (scripts,
  `printf %s "$SENHA" | dwnvr -hash-password`), ou só do terminal.
- **`x/term` ou syscall** para digitar a senha sem mostrar.
- **A ordem com o limite de tentativas**: o limite antes, ou o hash já com a
  fila do item 7.

## O que muda nas docs e nos testes

- **README**, passo 5 (o login): como gerar o hash e onde colar. A frase da
  dependência única só muda se a escolha for o Argon2id.
- **`dwnvr.example.yaml`**: o comentário do `server.password`.
- **`docs/operacao.md`**, "Trocar a senha e derrubar as sessões": gerar o hash
  da senha nova, e o aviso de que gerar outro da mesma senha também derruba.
- **`docs/api.md`**, §Sessão: a chave sai do valor guardado, hash ou texto.
- **Testes**: senha certa e errada com hash; o texto puro continua valendo;
  hash malformado; o usuário errado também passa pelo hash; a sessão derivada
  do hash; o valor que a flag gera é aceito no login.

## Já resolvido no mesmo estudo

- **O `chmod 600`** do `dwnvr.yaml` e do `go2rtc.yaml`, no passo 2 do README.
  O go2rtc continua lendo e, ao reescrever o arquivo pela interface web,
  mantém o modo e o dono.
- **A sessão amarrada à credencial** (`deriveSessionKey`): trocar a senha
  derruba as sessões abertas.
- **A assinatura do cookie fica HMAC-SHA256**, com a chave de 32 bytes do
  `.session-secret`. Forjar um cookie exige acertar a chave ou a assinatura,
  2^256 possibilidades cada; um computador quântico baixaria a busca da chave
  para 2^128. O HMAC nem depende de o SHA-256 resistir a colisão. É o mesmo
  primitivo do JWT HS256 e da assinatura das chamadas à API da AWS (SigV4).
  HMAC-SHA512, assinatura Ed25519 ou RSA e cookie cifrado (AES-GCM) não trazem
  ganho prático.

## Como medir de novo

**Tamanho.** Um arquivo `zz_medir.go` em `cmd/dwnvr`, com um `init()` que
chama a função do algoritmo só quando uma variável de ambiente existe, para o
linker não descartar o código. Compilado com as flags do Dockerfile
(`CGO_ENABLED=0`, `-trimpath`, `-ldflags="-s -w"`), para arm64 e amd64, contra
o mesmo commit sem o arquivo. "Código" é a soma das seções do ELF, sem as
NOBITS; "arquivo" é o tamanho em disco.

**Custo.** O programa abaixo, num módulo à parte, fora do repositório, com o
`golang.org/x/crypto`. Compilado no notebook para o Orange Pi e rodado lá com
prioridade baixa e teto de memória:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o bench-arm64 .

# no Orange Pi
systemd-run --user --unit=bench-hash --quiet --wait --pipe \
  -p MemoryMax=200M -p MemorySwapMax=0 \
  -p Nice=15 -p CPUSchedulingPolicy=batch -p IOSchedulingClass=idle \
  "$PWD/bench-arm64"
```

<details>
<summary><b>O programa de medição</b></summary>

```go
// Quanto custa um login com cada algoritmo de hash de senha: tempo de relógio
// (mediana), CPU do processo por vez (inclui o GC) e o pico de RSS do processo
// depois de cada linha.
package main

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"runtime"
	"sort"
	"syscall"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

func cpu() time.Duration {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

func pico() float64 {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return float64(ru.Maxrss) / 1024 // KiB -> MiB
}

func medir(nome string, n int, f func()) {
	f() // aquece
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	antes, c0 := ms.TotalAlloc, cpu()
	ds := make([]time.Duration, n)
	for i := range ds {
		t := time.Now()
		f()
		ds[i] = time.Since(t)
	}
	c := (cpu() - c0) / time.Duration(n)
	runtime.ReadMemStats(&ms)
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	ms2 := func(d time.Duration) float64 { return float64(d.Nanoseconds()) / 1e6 }
	fmt.Printf("%-30s %10.3f ms  cpu %10.3f ms  aloca %8.1f KiB  pico RSS %5.1f MiB\n",
		nome, ms2(ds[n/2]), ms2(c), float64(ms.TotalAlloc-antes)/float64(n)/1024, pico())
}

func main() {
	senha := "uma-senha-qualquer"
	salt := make([]byte, 16)
	rand.Read(salt)
	fmt.Printf("%s %s/%s, %d núcleos, pico RSS inicial %.1f MiB\n",
		runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), pico())
	chave := make([]byte, 32)
	medir("HMAC do cookie (cada pedido)", 10_000, func() {
		m := hmac.New(sha256.New, chave)
		m.Write([]byte("1760000000"))
		m.Sum(nil)
	})
	medir("PBKDF2-SHA256 100.000", 7, func() { pbkdf2.Key(sha256.New, senha, salt, 100_000, 32) })
	medir("PBKDF2-SHA256 600.000", 7, func() { pbkdf2.Key(sha256.New, senha, salt, 600_000, 32) })
	medir("bcrypt custo 10", 7, func() { bcrypt.GenerateFromPassword([]byte(senha), 10) })
	medir("Argon2id 7 MiB, t=5, p=1", 7, func() { argon2.IDKey([]byte(senha), salt, 5, 7*1024, 1, 32) })
	medir("Argon2id 19 MiB, t=2, p=1", 7, func() { argon2.IDKey([]byte(senha), salt, 2, 19*1024, 1, 32) })
}
```

</details>
