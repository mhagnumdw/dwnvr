<!--
  A aba Alerta do cadastro da câmera: ligar, as famílias que avisam, onde (a
  imagem inteira ou só as áreas marcadas), quando (HorariosDoAlerta) e para
  onde vai.

  Vem num chunk à parte (`import()`), como o editor de área: quem não tem
  detecção não baixa a aba. Quem confere o alerta incompleto ao salvar é o
  formulário, porque o "salvar" é dele; o motivo volta aqui em `erro`.
-->
<script lang="ts">
  import type { CameraAlert, EmailAlert, CameraArea, AlertWhere } from '../lib/api.ts';
  import { since } from '../lib/notices.ts';
  import { FAMILIES, FAMILY_NAMES, iconURL } from '../lib/icons.ts';
  import type { FamilyName } from '../lib/icons.ts';
  import type { Schedule } from '../lib/schedules.ts';
  import AlertSchedules from './AlertSchedules.svelte';
  import AreaTile from './AreaTile.svelte';

  interface Props {
    alert: CameraAlert | undefined;
    // Os três moram no formulário, e não no `alerta`: é o salvar dele que
    // confere.
    where?: AlertWhere;
    limitSchedules?: boolean;
    schedules?: Schedule[];
    areas: CameraArea[];
    cam: string;
    // A URL do quadro da câmera, vazia enquanto ele não chega: o fundo dos
    // ladrilhos das áreas.
    frame: string;
    timezone: string;
    // O "Para onde vai", do /api/cameras. Nulo contra servidor anterior a ele.
    byEmail: EmailAlert | null;
    error?: string;
  }

  let {
    alert = $bindable(),
    where = $bindable('tudo'),
    limitSchedules = $bindable(false),
    schedules = $bindable([]),
    areas,
    cam,
    frame,
    timezone,
    byEmail,
    error = $bindable(''),
  }: Props = $props();

  // A aba só mexe nas famílias e nas áreas com o alerta já criado pelo liga;
  // isto só poupa o `!`.
  const current = (): CameraAlert => alert ?? { enabled: false };

  const isEnabled = $derived(!!alert?.enabled);
  const families = $derived(alert?.families ?? []);
  const checkedList = $derived(alert?.areas ?? []);

  // Ligar pela primeira vez começa em `pessoa`, a de maior prioridade: com
  // `veiculo`, uma câmera voltada para a rua alertaria centenas de vezes por
  // dia. Desligar guarda o resto, para religar sem refazer.
  function enable(sim: boolean) {
    alert = { ...(alert ?? { families: ['pessoa'] }), enabled: sim };
    error = '';
  }

  function toggleFamily(f: FamilyName) {
    const newOne = families.includes(f) ? families.filter((x) => x !== f) : [...families, f];
    // Na ordem de prioridade, como o servidor lista.
    alert = { ...current(), families: FAMILY_NAMES.filter((x) => newOne.includes(x)) };
    error = '';
  }

  function toggleArea(id: string, sim: boolean) {
    const newOne = sim ? [...checkedList, id] : checkedList.filter((x) => x !== id);
    alert = {
      ...current(),
      areas: areas.flatMap((a) => (a.id && newOne.includes(a.id) ? [a.id] : [])),
    };
    error = '';
  }

  function chooseWhere(v: AlertWhere) {
    where = v;
    error = '';
  }

  const addresses = $derived(byEmail?.to ?? []);
  const noList = $derived(!!byEmail?.missing?.includes('alerts.email.to'));
  const noSmtp = $derived(!!byEmail?.missing?.includes('smtp'));
  // A seção smtp que falta, ou a recusada, com o motivo que o boot deu, e o
  // que mais falta para o alerta sair por e-mail.
  const smtpDisabled = $derived(
    (byEmail?.smtpError
      ? `${byEmail.smtpError}. Com a seção smtp consertada`
      : 'falta a seção smtp no dwnvr.yaml. Com ela') +
      (noList
        ? ' e a lista alerts.email.to, o alerta sai por e-mail.'
        : ', o alerta sai por e-mail para a lista alerts.email.to.'),
  );
  const nAddresses = $derived(
    `${addresses.length} ${addresses.length === 1 ? 'endereço' : 'endereços'}`,
  );
</script>

<label class="check">
  <input type="checkbox" checked={isEnabled} onchange={(e) => enable(e.currentTarget.checked)} />
  Avisar quando detectar
</label>

<div class="body" class:disabled={!isEnabled} inert={!isEnabled}>
  <div class="families" role="group" aria-label="famílias que avisam">
    {#each FAMILY_NAMES as f (f)}
      {@const on = families.includes(f)}
      <button
        type="button"
        class="ghost family"
        class:on
        aria-pressed={on}
        onclick={() => toggleFamily(f)}
      >
        <img src={iconURL(f, 14, FAMILIES[f].color)} alt="" width="14" height="14" />
        {FAMILIES[f].nome}
      </button>
    {/each}
  </div>

  <div class="where" role="radiogroup" aria-label="onde">
    <label class="check">
      <input
        type="radio"
        name="onde-do-alerta"
        checked={where === 'tudo'}
        onchange={() => chooseWhere('tudo')}
      />
      em qualquer lugar da imagem
    </label>
    <label class="check" class:without={!areas.length}>
      <input
        type="radio"
        name="onde-do-alerta"
        checked={where === 'areas'}
        disabled={!areas.length}
        onchange={() => chooseWhere('areas')}
      />
      só nas áreas marcadas
    </label>
    {#if !areas.length}
      <small class="muted">Desenhe as áreas na aba Áreas para escolher onde o alerta vale.</small>
    {:else if where === 'areas'}
      <div class="tiles" role="group" aria-label="áreas do alerta">
        {#each areas as a, i (a.id ?? `nova-${i}`)}
          {@const on = !!a.id && checkedList.includes(a.id)}
          <AreaTile
            {cam}
            area={a}
            {on}
            background={frame}
            onclick={() => {
              if (a.id) toggleArea(a.id, !on);
            }}
          />
        {/each}
      </div>
    {/if}
  </div>

  <AlertSchedules
    bind:limits={limitSchedules}
    bind:schedules
    {timezone}
    markIncomplete={!!error}
    onchange={() => (error = '')}
  />

  <div class="destination">
    <div class="title">Para onde vai</div>
    {#if addresses.length && byEmail?.failingSince}
      <p class="failing">
        Por e-mail, para {nAddresses}, mas falhando {since(byEmail.failingSince, Date.now())}: {byEmail.error}.
      </p>
      <p class="muted">E fica marcado na detecção (filtro "só alertas").</p>
    {:else if addresses.length}
      <div>
        Por e-mail, para {nAddresses}:
        <ul class="addresses">
          {#each addresses as e (e)}<li>{e}</li>{/each}
        </ul>
      </div>
      <p class="muted">E fica marcado na detecção (filtro "só alertas").</p>
    {:else}
      <p>Só fica marcado na detecção (filtro "só alertas").</p>
      {#if noSmtp}
        <small class="muted">O e-mail do dwnvr está desligado: {smtpDisabled}</small>
      {:else if noList}
        <small class="muted">
          Para receber por e-mail, ponha os endereços em alerts.email.to no dwnvr.yaml e reinicie o
          dwnvr.
        </small>
      {/if}
    {/if}
  </div>
  <small class="limit">
    Ficar sem aviso não quer dizer que não veio ninguém: o alerta só sai do que a detecção marcar, e
    ela não pega toda chegada.
  </small>
</div>

{#if error}<p class="small error" role="alert">{error}</p>{/if}

<style>
  .body {
    display: grid;
    gap: 14px;
  }
  .body.disabled {
    opacity: 0.45;
  }

  .families {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  /* O mesmo botão das famílias de Detecções, com o rótulo sempre à vista. */
  .family {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 8px 11px;
    color: var(--dim);
  }
  .family.on {
    color: var(--fg);
    border-color: var(--accent);
  }
  .family img {
    display: block;
    flex: none;
  }
  .family:not(.on) img {
    opacity: 0.4;
  }

  .where {
    display: grid;
    gap: 10px;
  }
  .tiles {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 6px;
  }

  label.check {
    display: flex;
    align-items: center;
    gap: 9px;
    color: var(--fg);
    font-size: 15px;
  }
  label.check.without {
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

  .destination {
    display: grid;
    gap: 8px;
    padding-top: 12px;
    border-top: 1px solid var(--line);
    font-size: 15px;
  }
  .destination p {
    margin: 0;
  }
  .title {
    color: var(--dim);
    font-size: 13px;
    font-weight: 600;
  }
  .addresses {
    margin: 2px 0 0;
    padding: 0;
    list-style: none;
    color: var(--dim);
    font-family: ui-monospace, monospace;
    font-size: 13px;
    overflow-wrap: anywhere;
  }
  .failing {
    color: var(--bad);
    overflow-wrap: anywhere;
  }

  .limit {
    color: var(--warn);
  }
  .error {
    margin: 0;
    color: var(--bad);
  }
</style>
