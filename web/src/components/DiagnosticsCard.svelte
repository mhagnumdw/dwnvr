<script lang="ts">
  // A Diagnostics lookup card: it starts closed, opens with a tap and shows
  // groups of label and value, with a "copiar" that joins everything into text
  // to send in a chat. It is the format of "Este navegador" and "Este
  // servidor"; whoever uses it decides what to collect, and only collects with
  // `isOpen`.
  //
  // `text` returns what "copiar" carries. `children` comes after the groups,
  // for what does not fit label and value - the log lines, for example.
  // `subtitle`, optional, goes under the title when the card is open - the
  // server's "Coletado em", for example.
  import type { Snippet } from 'svelte';
  import { copy } from '../lib/browser.ts';
  import Icon from './Icon.svelte';
  import type { CardGroup } from '../lib/browser.ts';

  interface Props {
    title: string;
    subtitle?: string;
    groups: CardGroup[];
    isOpen?: boolean;
    text: () => string;
    children?: Snippet;
  }

  let { title, subtitle = '', groups, isOpen = $bindable(false), text, children }: Props = $props();

  let copied = $state('');

  async function onCopy() {
    const ok = await copy(text());
    copied = ok ? 'copiado' : 'não deu para copiar';
    setTimeout(() => (copied = ''), 2500);
  }
</script>

<div class="card diag">
  <div class="row">
    <!-- The whole header is the button, not just the arrow: on the phone it is
         the target a finger finds. The "copiar" stays outside it, otherwise
         tapping to copy would also close the card. -->
    <button class="open row" onclick={() => (isOpen = !isOpen)} aria-expanded={isOpen}>
      <span class="arrow-card"><Icon name={isOpen ? 'chevronDown' : 'chevronDir'} size={14} /></span
      >
      <strong>{title}</strong>
    </button>
    {#if isOpen && groups.length}
      {#if copied}<span class="muted small">{copied}</span>{/if}
      <button class="copy" onclick={onCopy}>copiar</button>
    {/if}
  </div>
  <!-- Outside the button: inside it, the second line would change the header
       height and the title would jump on open. -->
  {#if isOpen && subtitle}<p class="subtitle muted small mono">{subtitle}</p>{/if}
  {#if isOpen && groups.length}
    <div class="groups">
      {#each groups as g (g.title)}
        <section>
          <p class="title muted">{g.title}</p>
          <dl>
            <!-- By position, not by label: part of the label comes from the
                 server (the sensor name), and two equal ones would make Svelte
                 stop drawing the whole card. -->
            {#each g.items as i, n (n)}
              <div class="item small">
                <dt class="muted">{i.label}</dt>
                <dd class="mono" class:alert={i.alert}>{i.value}</dd>
              </div>
            {/each}
          </dl>
        </section>
      {/each}
    </div>
    {@render children?.()}
  {/if}
</div>

<style>
  .diag:not(.row) {
    display: grid;
    gap: 10px;
  }

  /* One column on the phone, two when it fits. Groups do not split across
     columns: a half "Vídeo" would separate H.265 from MediaSource. */
  .groups {
    display: grid;
    gap: 14px;
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  }
  dl {
    margin: 0;
    display: grid;
    gap: 2px;
  }
  .title {
    margin: 0 0 4px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-size: 11px;
  }
  /* A button that does not look like one: it reads like the title of the other
     cards. The 44px touch target stays; the negative margin gives the height
     back so the closed card is not taller than its neighbors. */
  .open {
    flex: 1;
    gap: 8px;
    margin: -8px 0;
    padding: 0;
    background: none;
    border: none;
    color: inherit;
    font: inherit;
    text-align: left;
  }
  .open:hover:not(:disabled) {
    border-color: transparent;
  }
  /* The same margin as the header: without it, the "copiar" that appears on
     open would make the row 16px taller, and the title would drop. */
  .copy {
    margin: -8px 0;
  }
  /* Aligned with the title, after the arrow (12px) and the gap (8px), and
     pulled close to it. */
  .subtitle {
    margin: -6px 0 0;
    padding-left: 20px;
  }
  .arrow-card {
    display: flex;
    width: 14px;
    color: var(--dim);
  }
  .item {
    display: flex;
    gap: 10px;
    justify-content: space-between;
  }
  dd {
    margin: 0;
    text-align: right;
    overflow-wrap: anywhere;
  }
  .alert {
    color: var(--warn);
  }
</style>
