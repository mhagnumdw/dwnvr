<script>
  // Relógio da reprodução que também aceita um horário digitado.
  //
  // É um <input> o tempo todo, e não um texto que vira campo no clique: o
  // Safari do iPhone só abre o teclado quando o foco nasce do próprio toque, e
  // um focus() chamado depois de trocar o elemento chega tarde demais.
  //
  // A máscara lê o que foi digitado em três grupos, hora, minuto e segundo.
  // Hora com um dígito completa à esquerda (1 = 01h); minuto e segundo com um
  // dígito completam à direita (3 = 30), que é o que se quer dizer ao digitar
  // "17:3". Os dois-pontos aparecem sozinhos quando o grupo não aceita mais
  // dígito, e ponto, vírgula e espaço valem como dois-pontos: o teclado
  // numérico do celular não tem ":".
  import { hhmmss } from '../lib/format.js';

  /** @type {{ ms: number, onir: (segundosDoDia: number) => void }} */
  let { ms, onir } = $props();

  let input;
  let editando = $state(false);
  let texto = $state('');
  let grupos = $state(['']);
  // O que o player mostrava ao abrir o campo, para servir de placeholder: o
  // relógio de fundo continua andando, e o placeholder piscando distrai.
  let antes = $state('');

  const cheio = (g, i) => g[i].length === 2 || (i === 0 && g[i].length === 1 && +g[i] >= 3);

  // `apagando` segura os dois-pontos automáticos do fim: sem ele, apagar o ":"
  // o faria voltar na mesma hora.
  function mascara(bruto, apagando) {
    const g = [''];
    let recusou = false;
    for (const c of bruto.replace(/[.,;\s]/g, ':')) {
      let i = g.length - 1;
      if (c === ':') {
        if (g[i] !== '' && i < 2) g.push('');
        continue;
      }
      if (c < '0' || c > '9') continue;
      if (cheio(g, i)) {
        if (i === 2) {
          recusou = true;
          continue;
        }
        g.push('');
        i++;
      }
      // Dígito que só formaria horário impossível é recusado aqui, e não na
      // hora de ir: "24" ou o "7" de "17:7" (70 min) nunca chegam a aparecer.
      const novo = g[i] + c;
      const ok = i === 0 ? novo.length === 1 || +novo <= 23 : novo.length === 2 || c <= '5';
      if (!ok) {
        recusou = true;
        continue;
      }
      g[i] = novo;
    }
    const i = g.length - 1;
    if (!apagando && i < 2 && g[i] !== '' && cheio(g, i)) g.push('');
    return { g, recusou };
  }

  function segundos(g) {
    if (!g[0]) return null;
    const dezena = (x) => (x.length === 1 ? +x * 10 : +x);
    return +g[0] * 3600 + dezena(g[1] ?? '') * 60 + dezena(g[2] ?? '');
  }

  // O resto de HH:MM:SS em cinza depois do que já foi digitado: "17:3" ganha
  // "0:00", e dá para ver que o Enter leva a 17:30:00.
  const fantasma = $derived.by(() => {
    if (!grupos[0]) return '';
    const i = grupos.length - 1;
    let s = grupos[i].length === 0 ? '00' : i > 0 && grupos[i].length === 1 ? '0' : '';
    for (let k = i + 1; k < 3; k++) s += ':00';
    return s;
  });

  function abrir() {
    antes = hhmmss(ms);
    texto = '';
    grupos = [''];
    editando = true;
  }

  function digitar(e) {
    const m = mascara(e.currentTarget.value, e.inputType?.startsWith('delete'));
    texto = m.g.join(':');
    grupos = m.g;
    // Direto no DOM: quando o dígito é recusado o texto não muda, o Svelte não
    // teria o que atualizar e o caractere recusado ficaria no campo.
    e.currentTarget.value = texto;
    if (m.recusou) {
      input.animate(
        [{ transform: 'none' }, { transform: 'translateX(-2px)' }, { transform: 'translateX(2px)' }, { transform: 'none' }],
        { duration: 180 },
      );
    }
  }

  function tecla(e) {
    if (e.key === 'Enter') {
      const s = segundos(grupos);
      if (s !== null) onir(s);
      input.blur();
    } else if (e.key === 'Escape') {
      input.blur();
    }
  }
</script>

<span class="relogio" class:editando>
  {#if editando}
    <span class="espelho" aria-hidden="true">{texto}<i>{fantasma}</i></span>
  {/if}
  <input
    bind:this={input}
    class="mono"
    value={editando ? texto : ms ? hhmmss(ms) : '--:--:--'}
    placeholder={editando ? antes : ''}
    disabled={!ms}
    inputmode="decimal"
    enterkeyhint="go"
    autocomplete="off"
    spellcheck="false"
    aria-label="horário; digite outro para ir até ele"
    title="Clique para ir a um horário"
    onfocus={abrir}
    onblur={() => (editando = false)}
    oninput={digitar}
    onkeydown={tecla}
  />
</span>

<style>
  /* Em repouso e editando é a mesma caixa: nada ao lado se mexe quando o campo
     abre. A largura cabe "00:00:00" com folga; os dígitos têm largura fixa. */
  .relogio {
    position: relative;
    display: inline-block;
    width: calc(8ch + 18px);
    height: 44px;
    font-size: 16px;
  }

  input,
  .espelho {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    margin: 0;
    /* 16px também evita o zoom automático do Safari ao focar o campo. */
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
  input:hover:not(:disabled) { border-color: var(--line); }
  input:disabled { cursor: default; color: var(--fg); }
  input:focus-visible { outline: none; }
  input::placeholder { color: var(--dim); opacity: 0.55; }

  .editando input { border-color: var(--accent); }

  .espelho {
    border: 1px solid transparent;
    background: var(--panel-2);
    color: transparent;
    pointer-events: none;
    font-variant-numeric: tabular-nums;
  }
  .espelho i {
    font-style: normal;
    color: var(--dim);
    opacity: 0.6;
  }
</style>
