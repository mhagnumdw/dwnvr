<script lang="ts">
  // An HH:MM time field, with the clock's mask (lib/hourMask.ts): "22" is
  // already 22:00 and "17:3" is 17:30, with the rest in gray showing that. The
  // value is the minute of the day, or null with the field empty: `value` is
  // the starting one, and `onchange` receives each change.
  import { untrack } from 'svelte';
  import { hourOfMinute } from '../lib/format.ts';
  import { ghost as rest, mask, shake, secondsOfDay } from '../lib/hourMask.ts';

  interface Props {
    value: number | null;
    onchange: (minute: number | null) => void;
    label: string;
    // Inside a form, Enter already submits it, and whoever uses it may have
    // nothing else to do.
    onenter?: () => void;
    // Focus on open, for the first field of the popover.
    focusNow?: boolean;
  }

  let { value, onchange, label, onenter, focusNow = false }: Props = $props();

  const format = (m: number | null) => (m === null ? '' : hourOfMinute(m));

  let input: HTMLInputElement | undefined;
  // Only the value from when the field is born: from then on the person writes,
  // and the popover that uses it mounts new fields on each opening.
  let current = untrack(() => value);
  const initial = format(current);
  let text = $state(initial);
  let typed = $state<string[]>(initial ? initial.split(':') : ['']);
  let isFocused = $state(false);

  const ghost = $derived(isFocused ? rest(typed, 2) : '');

  $effect(() => {
    if (focusNow) input?.focus();
  });

  function typeIn(e: Event & { currentTarget: HTMLInputElement }) {
    const m = mask(
      e.currentTarget.value,
      e instanceof InputEvent && e.inputType.startsWith('delete'),
      2,
    );
    text = m.g.join(':');
    typed = m.g;
    // Straight on the DOM, as in the clock: a rejected digit does not change
    // the text.
    e.currentTarget.value = text;
    if (m.rejected) shake(input);
    const s = secondsOfDay(m.g);
    current = s === null ? null : s / 60;
    onchange(current);
  }

  // On blur, the field shows the whole time: "17:3" becomes "17:30".
  function leave() {
    isFocused = false;
    text = format(current);
    typed = text ? text.split(':') : [''];
  }
</script>

<label class="field">
  <span class="label">{label}</span>
  <span class="box">
    {#if isFocused}
      <span class="mirror mono" aria-hidden="true">{text}<i>{ghost}</i></span>
    {/if}
    <input
      bind:this={input}
      class="mono"
      value={text}
      placeholder="--:--"
      inputmode="decimal"
      enterkeyhint="done"
      autocomplete="off"
      spellcheck="false"
      onfocus={() => (isFocused = true)}
      onblur={leave}
      oninput={typeIn}
      onkeydown={(e) => {
        if (e.key === 'Enter') {
          leave();
          onenter?.();
        }
      }}
    />
  </span>
</label>

<style>
  .field {
    display: grid;
    gap: 4px;
    font-size: 13px;
    color: var(--dim);
  }

  /* The background belongs to the box, and the input is transparent over the
     mirror: the typed text and the gray rest sit on the same baseline. */
  .box {
    position: relative;
    display: block;
    height: 44px;
    border-radius: 8px;
    background: var(--panel-2);
  }

  input,
  .mirror {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    margin: 0;
    /* 16px also avoids Safari's automatic zoom when the field gets focus. */
    font-size: 16px;
    line-height: 24px;
    padding: 9px 12px;
    border-radius: 8px;
    white-space: pre;
  }

  input {
    z-index: 1;
    background: transparent;
  }
  input:focus-visible {
    outline: none;
    border-color: var(--accent);
  }
  input::placeholder {
    color: var(--dim);
    opacity: 0.55;
  }

  .mirror {
    border: 1px solid transparent;
    color: transparent;
    pointer-events: none;
  }
  .mirror i {
    font-style: normal;
    color: var(--dim);
    opacity: 0.6;
  }
</style>
