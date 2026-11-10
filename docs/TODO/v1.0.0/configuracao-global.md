# Tela de configuração global

**Status (10/11/2026): planejado.** O estudo de base é o
[TODO_tela-de-configuracao-global.md](../TODO_tela-de-configuracao-global.md),
de 07/10/2026, e o primeiro passo desta etapa é revisá-lo.

## O que é

Uma tela, só do admin, para mudar o que hoje só se muda no `dwnvr.yaml`, sem
editar arquivo e sem reiniciar.

## O que o estudo de 07/10 decidiu

- **O critério das quatro perguntas**, que vale para qualquer campo: errar o
  valor derruba a gravação ou obriga a mexer no compose junto? É escolha de
  uso, que se muda olhando o efeito? É número medido de um `parametros.go`?
  Vale sem reiniciar?
- **Entram** o `storage.minFreeMB`, o `defaults.maxDays`, o `defaults.detect`,
  o `defaults.detectSensibilidade` e o `DETECT_THREADS` do `dwnvr-detect`.
  **Ficam de fora** a ligação entre containers (`server.listen`, `go2rtc.*`,
  `storage.root`, `detector.url`), o que depende de cada câmera (cota, áudio,
  duração do segmento, tolerância de travamento, mecanismo da detecção), a
  senha do dono e os números medidos.
- **O valor da tela mora num arquivo próprio**, gravado por ela ao lado do
  `cameras.json`, e sai do `dwnvr.yaml`: o dwnvr nunca reescreve o yaml, para
  não apagar os comentários de quem o escreveu.
- **Conclusão: "não agora"**, porque a herança dos defaults era a parte cara.

## O que mudou desde então

- **A herança entrou** no PR #47, em 22/10/2026: salvar a câmera não grava mais
  nela o default que ela não tinha (`config.TiraDefaultsNovos`). O bloqueio
  principal caiu. Ficou o que é do formulário: ele não mostra o que é herdado e
  o que é próprio, e não consegue fixar numa câmera um valor igual ao default
  de hoje.
- **Seções novas no `dwnvr.yaml`**, que o estudo não avaliou: `smtp`,
  `alerts.email.to`, `server.publicUrl`, `server.loginAttemptsPerMinute` e
  `server.trustedProxies`.
- **Campos que este plano cria:** o idioma padrão da instância, na Etapa 5
  ([i18n](i18n.md)), e, se a Etapa 4 ([paleta](paleta.md)) quiser, a paleta.

## Primeira leitura dos campos novos

A confirmar na revisão, pelas quatro perguntas:

| Campo | Leitura | Por quê |
| --- | --- | --- |
| `alerts.email.to` | entra | É escolha de uso, muda com quem mora na casa, e errar não para a gravação |
| `smtp.*` | entra | Errar não para a gravação, e o e-mail de teste do Diagnóstico mostra o efeito na hora. A senha é só de escrita: a tela nunca a devolve |
| `server.publicUrl` | entra | Só monta o botão do e-mail de alerta, que abre a gravação no instante |
| `server.loginAttemptsPerMinute` | a ver | Errar não para a gravação, mas é um número de segurança |
| `server.trustedProxies` | fica | Confiar de menos faz o limite do login tratar todo mundo como o proxy, e uma pessoa trava as outras; confiar demais deixa qualquer um forjar o endereço. E depende de como o proxy foi montado, fora da tela |

## Depende de

- **Da conversão**: os campos e o arquivo nascem em en-US.
- **Do i18n**: a tela nasce nos dois idiomas, e o idioma padrão entra nela.
