<!--
  A person's photo, in a circle. Without a photo, the initials of the name in a
  colored circle, with no file at all: the color comes from the user, so it is
  always the same for the same person.

  <Avatar person={{ username, name, avatar }} size={32} />
-->
<script lang="ts">
  import { mediaURL } from '../lib/api.ts';

  interface Props {
    // Null before the session arrives: then the empty circle is drawn.
    person: { username?: string; name?: string; avatar?: string } | null | undefined;
    size?: number;
  }

  let { person, size = 32 }: Props = $props();

  // Dark enough for the white of the initials to read well.
  const COLORS = ['#8957e5', '#1f6feb', '#238636', '#9e6a03', '#bf4b8a', '#0e7490'] as const;

  function colorOf(user = '') {
    let h = 0;
    for (const ch of user) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
    return COLORS[h % COLORS.length] ?? COLORS[0];
  }

  // The first letter of the first and last names: "Maria Clara Souza" is MS.
  function initials(name = '') {
    const p = name.trim().split(/\s+/);
    return ((p[0]?.[0] ?? '') + (p.length > 1 ? (p.at(-1)?.[0] ?? '') : '')).toUpperCase();
  }

  // A photo that failed to load (deleted from another device, server down)
  // falls back to the initials, instead of the broken-image icon.
  let failed = $state('');
  const photo = $derived(person?.avatar && failed !== person.avatar ? person.avatar : '');
</script>

<span
  class="av"
  style:width="{size}px"
  style:height="{size}px"
  style:font-size="{Math.round(size * 0.38)}px"
  style:background={photo ? 'var(--panel-2)' : colorOf(person?.username)}
  aria-hidden="true"
>
  {#if photo}
    <img src={mediaURL.avatar(photo)} alt="" onerror={() => (failed = photo)} />
  {:else}
    {initials(person?.name)}
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
