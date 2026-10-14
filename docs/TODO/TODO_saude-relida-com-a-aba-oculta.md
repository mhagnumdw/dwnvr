# TODO - Câmeras e Diagnóstico releem a saúde com a aba oculta

As telas Câmeras e Diagnóstico mostram o estado de cada câmera e do disco, e
para isso perguntam ao dwnvr de novo a cada 5 segundos. O resto da interface
para de perguntar quando você troca de aba ou bloqueia o celular, porque
ninguém está olhando. Essas duas telas não param: deixadas abertas numa aba
esquecida, seguem perguntando dia e noite, 17.280 vezes por dia, e o aparelho
que grava responde a cada uma sem que ninguém veja a resposta.

Achado em 14/10/2026, no levantamento do último acesso de cada pessoa na tela
Usuários (worktree `ultimo-acesso`).

**Status: não corrigido. Não medido.**

## O que o código faz

- O `pollHealth` (`web/src/lib/state.svelte.ts`) é um `setInterval` de
  `HEALTH_POLL_MS` (5 s) que chama o `loadHealth` sem olhar o
  `document.hidden`. A tela Câmeras o liga sempre, e a Diagnóstico, para o
  admin.
- Ele está assim desde o primeiro commit da interface (`62989e6`, 08/08/2026).
  O `efc43ba` só juntou o intervalo num lugar. Nenhum commit nem comentário diz
  que ler com a aba oculta é de propósito, e nada na tela depende disso: o
  `health` não acumula histórico entre leituras, e nada avisa em segundo
  plano.
- Os dois polls que vieram depois já param com a aba oculta: o card do servidor
  no Diagnóstico (`a08f0f3`, porque cada leitura grava no storage) e o sino do
  header, no `vigiarAvisos` (`e724728`).

## O custo de cada leitura

O `handleHealth` (`internal/api/server.go`) faz, a cada chamada:

- duas leituras do espaço em disco (`FreeBytes` e `TotalBytes`);
- o `OrphanBytes`: um `ReadDir` da pasta de gravações e, para cada câmera
  removida que deixou gravação, o `Scan` dos arquivos de índice dela (algumas
  dezenas de arquivos por câmera);
- o `mgr.Status()` duas vezes.

Cada leitura é barata, mas a aba esquecida a repete para sempre. Com câmera
órfã no disco, cada leitura ainda relê os índices dela.

Também afeta o último acesso que a tela Usuários vai mostrar: um admin com uma
dessas abas esquecida fica sempre com "acesso agora".

## A ideia

Fazer o `pollHealth` como os outros dois: no tique, só ler com a aba visível;
no `visibilitychange`, ler na hora ao voltar. O texto "última leitura há…" do
rodapé continua certo, porque a leitura sai assim que a aba volta.

## Prós

- A aba esquecida deixa de custar ao aparelho que grava.
- Os três polls da saúde passam a seguir a mesma regra.
- O último acesso do admin deixa de ser falso.

## Contras

- Nenhum que eu tenha achado. Quem volta para a aba vê o dado de antes por
  uma fração de segundo, até a leitura nova chegar, como já acontece no sino.

## Vale a pena agora?

Sim, junto ou logo depois do último acesso: é uma mudança de poucas linhas,
sem decisão de produto, e tira o "acesso agora" falso do admin. Sozinha, sem o
último acesso, não é urgente: o custo existe, mas ninguém o mediu, e só aparece
com uma aba do admin esquecida aberta.
