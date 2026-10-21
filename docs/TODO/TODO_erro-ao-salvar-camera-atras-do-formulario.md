# TODO - erro ao salvar câmera aparece atrás do formulário

Achado em 21/10/2026, ao testar pela tela a recusa de vírgula no ID da câmera:
um stream do go2rtc chamado `cam,virgula`, cadastro aberto, salvar.

**Status: não corrigido.**

## O que acontece

Quando o servidor recusa o `POST /api/cameras` (400), o formulário continua
aberto, e a mensagem vai para o `errbox` do topo da página de Câmeras. O
`Modal` escurece a página inteira, então a mensagem fica atrás do fundo
escuro, apagada, longe do botão "salvar" e fácil de não ver. Para quem clicou,
parece que o "salvar" não fez nada.

Não vem da regra da vírgula: vale para qualquer recusa do servidor ao salvar.
Hoje ela é rara, porque os campos numéricos já são barrados no navegador pelo
`min`/`max` das `faixas`, e quase tudo que o servidor recusaria não chega a ser
enviado.

As outras ações da tela que usam o mesmo `error` (remover a câmera, apagar as
gravações, reiniciar o go2rtc) fecham o diálogo antes de chamar a API, então
lá a mensagem aparece na página, à vista. O problema é só do `salvar()`, o
único que deixa o diálogo aberto quando falha.

## Como implementar

Um estado de erro próprio do formulário, mostrado dentro do `Modal`, perto do
rodapé com o "salvar". É o que o `Usuarios.svelte` já faz no cadastro de
usuário (`erroNovo`, um `<p class="small erro">` dentro do formulário). Em
`web/src/routes/Cameras.svelte`:

- `salvar()` grava em `erroForm`, e não em `error`;
- `erroForm` se limpa ao abrir o formulário (`novo`, `editar`) e a cada envio;
- o `<p>` fica logo acima do rodapé do formulário, visível em qualquer aba.

## Prós

- A pessoa lê o motivo onde está olhando e corrige na hora, que é o motivo de
  a API recusar em vez de trocar pelo padrão.
- Mesmo padrão do cadastro de usuários.

## Contras

- Um estado a mais na tela de Câmeras.

## Vale a pena agora?

Não tem pressa: a recusa do servidor ao salvar é rara. Vale fazer junto da
próxima mudança no formulário da câmera.
