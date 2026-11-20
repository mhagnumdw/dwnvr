<script lang="ts">
  // Playback clock that also accepts a typed time.
  //
  // It is an <input> all the time, not a text that becomes a field on click:
  // iPhone Safari only opens the keyboard when focus comes from the tap itself,
  // and a focus() called after swapping the element arrives too late.
  //
  // The mask lives in lib/hourMask.ts, the same one as the time fields in
  // Detections. Without the seconds (`segundos={false}`), the clock shows and
  // accepts only HH:MM, and `ongo` receives second zero of the typed minute.
  import { hhmm, hhmmss } from '../lib/format.ts';
  import { ghost as rest, mask, shake, secondsOfDay } from '../lib/hourMask.ts';

  interface Props {
    ms: number;
    ongo: (secondsOfDay: number) => void;
    segundos?: boolean;
  }

  let { ms, ongo, segundos = true }: Props = $props();

  const groups = $derived(segundos ? 3 : 2);
  const format = (v: number) => (segundos ? hhmmss(v) : hhmm(v));

  let input: HTMLInputElement | undefined;
  let editing = $state(false);
  let text = $state('');
  let typed = $state<string[]>(['']);
  // What the player showed when the field opened, to serve as the placeholder:
  // the background clock keeps running, and a flickering placeholder distracts.
  let before = $state('');

  const ghost = $derived(rest(typed, groups));

  function openNow() {
    before = format(ms);
    text = '';
    typed = [''];
    editing = true;
  }

  function typeIn(e: Event & { currentTarget: HTMLInputElement }) {
    const m = mask(
      e.currentTarget.value,
      e instanceof InputEvent && e.inputType.startsWith('delete'),
      groups,
    );
    text = m.g.join(':');
    typed = m.g;
    // Straight on the DOM: when a digit is rejected the text does not change,
    // Svelte has nothing to update, and the rejected character would stay in
    // the field.
    e.currentTarget.value = text;
    if (m.rejected) shake(input);
  }

  function keyHandler(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      const s = secondsOfDay(typed);
      if (s !== null) ongo(s);
      input?.blur();
    } else if (e.key === 'Escape') {
      input?.blur();
    }
  }
</script>

<span class="clock" class:editing class:no-seconds={!segundos}>
  {#if editing}
    <span class="mirror" aria-hidden="true">{text}<i>{ghost}</i></span>
  {/if}
  <input
    bind:this={input}
    class="mono"
    value={editing ? text : ms ? format(ms) : segundos ? '--:--:--' : '--:--'}
    placeholder={editing ? before : ''}
    disabled={!ms}
    inputmode="decimal"
    enterkeyhint="go"
    autocomplete="off"
    spellcheck="false"
    aria-label="horário; digite outro para ir até ele"
    title="Clique para ir a um horário"
    onfocus={openNow}
    onblur={() => (editing = false)}
    oninput={typeIn}
    onkeydown={keyHandler}
  />
</span>

<style>
  /* At rest and editing it is the same box: nothing beside it moves when the
     field opens. The width fits "00:00:00" with room to spare; the digits have
     fixed width. */
  .clock {
    position: relative;
    display: inline-block;
    width: calc(8ch + 18px);
    height: 44px;
    font-size: 16px;
  }
  .clock.no-seconds {
    width: calc(5ch + 18px);
  }

  input,
  .mirror {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    margin: 0;
    /* 16px also avoids Safari's automatic zoom when the field gets focus. */
    font: inherit;
    font-size: 16px;
    line-height: 24px;
    padding: 9px 8px;
    border-radius: 8px;
    white-space: pre;
  }

  input {
    z-index: 1;
    background: transparent;
    border: 1px solid transparent;
    cursor: text;
  }
  input:hover:not(:disabled) {
    border-color: var(--line);
  }
  input:disabled {
    cursor: default;
    color: var(--fg);
  }
  input:focus-visible {
    outline: none;
  }
  input::placeholder {
    color: var(--dim);
    opacity: 0.55;
  }

  .editing input {
    border-color: var(--accent);
  }

  .mirror {
    border: 1px solid transparent;
    background: var(--panel-2);
    color: transparent;
    pointer-events: none;
    font-variant-numeric: tabular-nums;
  }
  .mirror i {
    font-style: normal;
    color: var(--dim);
    opacity: 0.6;
  }
</style>
