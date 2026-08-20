<!--
  A mesma frase aparece no ao vivo, nas gravações e no cadastro. Aqui ela existe
  uma vez só - e junto com ela o caminho para resolver o problema, que era o que
  faltava: a mensagem antiga dizia o que não havia e escondia onde se cadastra.

    <SemCameras />              ao vivo, gravações
    <SemCameras link={false} /> no próprio cadastro, onde o link seria circular
-->
<script>
  let { link = true } = $props();
</script>

<div class="empty">
  <p>Nenhuma câmera cadastrada ainda.</p>
  {#if link}<a href="#cams">Cadastrar câmera</a>{/if}
</div>

<style>
  p { margin: 0 0 10px; }
</style>
