<!--
  A aba Alerta do cadastro da câmera: ligar, as famílias que avisam, onde (a
  imagem inteira ou só as áreas marcadas) e quando (HorariosDoAlerta).

  Vem num chunk à parte (`import()`), como o editor de área: quem não tem
  detecção não baixa a aba. Quem confere o alerta incompleto ao salvar é o
  formulário, porque o "salvar" é dele; o motivo volta aqui em `erro`.
-->
<script lang="ts">
  import type { AlertaDaCamera, AreaDaCamera, OndeDoAlerta } from '../lib/api.ts';
  import { FAMILIAS, NOMES_DAS_FAMILIAS, iconeURL } from '../lib/icones.ts';
  import type { NomeDaFamilia } from '../lib/icones.ts';
  import type { Horario } from '../lib/horarios.ts';
  import HorariosDoAlerta from './HorariosDoAlerta.svelte';

  interface Props {
    alerta: AlertaDaCamera | undefined;
    // Os três moram no formulário, e não no `alerta`: é o salvar dele que
    // confere.
    onde?: OndeDoAlerta;
    limitaHorarios?: boolean;
    horarios?: Horario[];
    areas: AreaDaCamera[];
    fuso: string;
    erro?: string;
  }

  let {
    alerta = $bindable(),
    onde = $bindable('tudo'),
    limitaHorarios = $bindable(false),
    horarios = $bindable([]),
    areas,
    fuso,
    erro = $bindable(''),
  }: Props = $props();

  // A aba só mexe nas famílias e nas áreas com o alerta já criado pelo liga;
  // isto só poupa o `!`.
  const atual = (): AlertaDaCamera => alerta ?? { enabled: false };

  const ligado = $derived(!!alerta?.enabled);
  const familias = $derived(alerta?.families ?? []);
  const marcadas = $derived(alerta?.areas ?? []);

  // Ligar pela primeira vez começa em `pessoa`, a de maior prioridade: com
  // `veiculo`, uma câmera voltada para a rua alertaria centenas de vezes por
  // dia. Desligar guarda o resto, para religar sem refazer.
  function liga(sim: boolean) {
    alerta = { ...(alerta ?? { families: ['pessoa'] }), enabled: sim };
    erro = '';
  }

  function alternaFamilia(f: NomeDaFamilia) {
    const nova = familias.includes(f) ? familias.filter((x) => x !== f) : [...familias, f];
    // Na ordem de prioridade, como o servidor lista.
    alerta = { ...atual(), families: NOMES_DAS_FAMILIAS.filter((x) => nova.includes(x)) };
    erro = '';
  }

  function alternaArea(id: string, sim: boolean) {
    const nova = sim ? [...marcadas, id] : marcadas.filter((x) => x !== id);
    alerta = {
      ...atual(),
      areas: areas.flatMap((a) => (a.id && nova.includes(a.id) ? [a.id] : [])),
    };
    erro = '';
  }

  function escolheOnde(v: OndeDoAlerta) {
    onde = v;
    erro = '';
  }
</script>

<label class="check">
  <input type="checkbox" checked={ligado} onchange={(e) => liga(e.currentTarget.checked)} />
  Avisar quando detectar
</label>

<div class="corpo" class:desligado={!ligado} inert={!ligado}>
  <div class="familias" role="group" aria-label="famílias que avisam">
    {#each NOMES_DAS_FAMILIAS as f (f)}
      {@const on = familias.includes(f)}
      <button
        type="button"
        class="ghost familia"
        class:on
        aria-pressed={on}
        onclick={() => alternaFamilia(f)}
      >
        <img src={iconeURL(f, 14, FAMILIAS[f].cor)} alt="" width="14" height="14" />
        {FAMILIAS[f].nome}
      </button>
    {/each}
  </div>

  <div class="onde" role="radiogroup" aria-label="onde">
    <label class="check">
      <input
        type="radio"
        name="onde-do-alerta"
        checked={onde === 'tudo'}
        onchange={() => escolheOnde('tudo')}
      />
      em qualquer lugar da imagem
    </label>
    <label class="check" class:sem={!areas.length}>
      <input
        type="radio"
        name="onde-do-alerta"
        checked={onde === 'areas'}
        disabled={!areas.length}
        onchange={() => escolheOnde('areas')}
      />
      só nas áreas marcadas
    </label>
    {#if !areas.length}
      <small class="muted">Desenhe as áreas na aba Áreas para escolher onde o alerta vale.</small>
    {:else if onde === 'areas'}
      <div class="lista">
        {#each areas as a, i (a.id ?? `nova-${i}`)}
          <label class="check">
            <input
              type="checkbox"
              checked={!!a.id && marcadas.includes(a.id)}
              onchange={(e) => {
                if (a.id) alternaArea(a.id, e.currentTarget.checked);
              }}
            />
            {a.name}
          </label>
        {/each}
      </div>
    {/if}
  </div>

  <HorariosDoAlerta
    bind:limita={limitaHorarios}
    bind:horarios
    {fuso}
    marcaIncompletos={!!erro}
    onmuda={() => (erro = '')}
  />

  <small class="muted">
    Fica marcado na detecção, e se vê em Detecções com o filtro "só alertas". Com a lista
    alerts.email.to no dwnvr.yaml, também vai por e-mail.
  </small>
  <small class="limite">
    O alerta avisa do que a detecção marcar, e ela não pega toda chegada: não é alarme.
  </small>
</div>

{#if erro}<p class="small erro" role="alert">{erro}</p>{/if}

<style>
  .corpo {
    display: grid;
    gap: 14px;
  }
  .corpo.desligado {
    opacity: 0.45;
  }

  .familias {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  /* O mesmo botão das famílias de Detecções, com o rótulo sempre à vista. */
  .familia {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 8px 11px;
    color: var(--dim);
  }
  .familia.on {
    color: var(--fg);
    border-color: var(--accent);
  }
  .familia img {
    display: block;
    flex: none;
  }
  .familia:not(.on) img {
    opacity: 0.4;
  }

  .onde {
    display: grid;
    gap: 10px;
  }
  .lista {
    display: grid;
    gap: 8px;
    padding-left: 27px;
  }

  label.check {
    display: flex;
    align-items: center;
    gap: 9px;
    color: var(--fg);
    font-size: 15px;
  }
  label.check.sem {
    color: var(--dim);
  }
  label.check input {
    width: 18px;
    height: 18px;
    min-height: 0;
    flex: none;
    accent-color: var(--accent);
  }
  small {
    font-size: 12px;
  }
  .limite {
    color: var(--warn);
  }
  .erro {
    margin: 0;
    color: var(--bad);
  }
</style>
