<!--
  A foto da pessoa, num círculo. Sem foto, as iniciais do nome num círculo
  colorido, sem arquivo nenhum: a cor sai do usuário, então é sempre a mesma
  para a mesma pessoa.

    <Avatar pessoa={{ usuario, nome, avatar }} tamanho={32} />
-->
<script>
  import { mediaURL } from '../lib/api.js';

  let { pessoa, tamanho = 32 } = $props();

  // Escuras o bastante para o branco das iniciais ler bem.
  const CORES = ['#8957e5', '#1f6feb', '#238636', '#9e6a03', '#bf4b8a', '#0e7490'];

  function corDe(usuario = '') {
    let h = 0;
    for (const ch of usuario) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
    return CORES[h % CORES.length];
  }

  // A primeira letra do primeiro e do último nome: "Maria Clara Souza" é MS.
  function iniciais(nome = '') {
    const p = nome.trim().split(/\s+/);
    return ((p[0]?.[0] ?? '') + (p.length > 1 ? p[p.length - 1][0] : '')).toUpperCase();
  }

  // A foto que não carregou (apagada por outro aparelho, servidor fora) cai
  // nas iniciais, em vez do ícone de imagem quebrada.
  let falhou = $state('');
  const foto = $derived(pessoa?.avatar && falhou !== pessoa.avatar ? pessoa.avatar : '');
</script>

<span
  class="av"
  style:width="{tamanho}px"
  style:height="{tamanho}px"
  style:font-size="{Math.round(tamanho * 0.38)}px"
  style:background={foto ? 'var(--panel-2)' : corDe(pessoa?.usuario)}
  aria-hidden="true"
>
  {#if foto}
    <img src={mediaURL.avatar(foto)} alt="" onerror={() => (falhou = foto)} />
  {:else}
    {iniciais(pessoa?.nome)}
  {/if}
</span>

<style>
  .av {
    display: inline-grid;
    place-items: center;
    flex: none;
    overflow: hidden;
    border-radius: 50%;
    color: #fff;
    font-weight: 600;
    letter-spacing: 0.02em;
    line-height: 1;
    user-select: none;
  }

  img {
    display: block;
    width: 100%;
    height: 100%;
  }
</style>
