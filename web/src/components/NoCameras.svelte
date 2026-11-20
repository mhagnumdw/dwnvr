<!--
  The same sentence shows up in Live, in Recordings and in the camera form. Here
  it exists only once - and with it the way to fix the problem, which was what
  was missing: the old message said what there was not and hid where to register
  it.

  <NoCameras /> live, recordings
  <NoCameras link={false} /> in the form itself, where the link would be
  circular

  The viewer does not see the link: the camera form is the admin's.
-->
<script lang="ts">
  import { isAdmin } from '../lib/state.svelte.ts';

  let { link = true } = $props();
</script>

<div class="empty">
  <p>Nenhuma câmera cadastrada ainda.</p>
  {#if link && isAdmin()}<a href="#cams">Cadastrar câmera</a>{/if}
</div>

<style>
  p {
    margin: 0 0 10px;
  }
</style>
