<!--
  A seção Horários da aba Alerta: a caixa que limita o alerta, a grade da
  semana e a lista digitada, que andam juntas. Digitar repinta a grade; pintar
  refaz a lista inteira na forma canônica (lib/horarios.ts).

  A caixa e a lista moram no formulário, como o "onde" do alerta: é o salvar
  dele que confere o que falta e apaga os horários com a caixa desmarcada.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import CampoHora from './CampoHora.svelte';
  import GradeDosHorarios from './GradeDosHorarios.svelte';
  import Icone from './Icone.svelte';
  import {
    DIAS,
    DIAS_LONGOS,
    cobertura,
    horariosDa,
    pintaHoras,
    resumoDasHoras,
    resumoDosDias,
  } from '../lib/gradeDaSemana.ts';
  import { DIA, MAX_HORARIOS, novoHorario, valido } from '../lib/horarios.ts';
  import type { Horario } from '../lib/horarios.ts';

  interface Props {
    limita?: boolean;
    horarios?: Horario[];
    // "America/Fortaleza (UTC−03:00)", do /api/health; vazio antes da leitura.
    fuso: string;
    // Depois de um salvar barrado: o horário pela metade fica vermelho.
    marcaIncompletos: boolean;
    onmuda: () => void;
  }

  let {
    limita = $bindable(false),
    horarios = $bindable([]),
    fuso,
    marcaIncompletos,
    onmuda,
  }: Props = $props();

  const coberturaDaLista = $derived(cobertura(horarios));

  // O horário sob o mouse ou com o foco, que a grade contorna.
  let realcado = $state<number | null>(null);
  const realce = $derived.by(() => {
    const h = horarios.find((x) => x.chave === realcado);
    return h && valido(h) ? cobertura([h]) : null;
  });

  // A linha com o foco: o "Até" antes do "De" só fica vermelho fora dela, porque
  // no meio da digitação ("1" a caminho de "18:00") ele ainda é provisório.
  let focada = $state<number | null>(null);

  // A lista acabou de ser refeita pela grade: as linhas novas piscam.
  let refeita = $state(false);

  let lista = $state<HTMLElement>();

  function muda(nova: Horario[], pelaGrade = false) {
    horarios = nova;
    refeita = pelaGrade;
    onmuda();
  }

  function pinta(d0: number, h0: number, d1: number, h1: number, marca: boolean) {
    const c = pintaHoras(coberturaDaLista, d0, h0, d1, h1, marca);
    // O que estava pela metade (sem dia ou sem hora) vai para o fim, para não
    // sumir.
    muda([...horariosDa(c), ...horarios.filter((h) => !valido(h))], true);
  }

  const troca = (chave: number, campos: Partial<Horario>) =>
    muda(horarios.map((h) => (h.chave === chave ? { ...h, ...campos } : h)));

  function alternaDia(h: Horario, d: number) {
    const days = h.days.includes(d)
      ? h.days.filter((x) => x !== d)
      : [...h.days, d].sort((a, b) => a - b);
    troca(h.chave, { days });
  }

  function remove(chave: number) {
    if (realcado === chave) realcado = null;
    muda(horarios.filter((h) => h.chave !== chave));
  }

  async function adiciona(...novos: Horario[]) {
    muda([...horarios, ...novos]);
    await tick();
    const ultima = lista?.lastElementChild as HTMLElement | null;
    ultima?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    // O horário vazio do "novo horário" já recebe o foco no primeiro dia.
    if (novos.some((h) => !valido(h)))
      ultima?.querySelector<HTMLElement>('.dia')?.focus({ preventScroll: true });
  }

  const TODO_DIA = [0, 1, 2, 3, 4, 5, 6];

  function nota(h: Horario) {
    if (valido(h))
      return { ruim: false, texto: `${resumoDosDias(h.days)} · ${resumoDasHoras(h.from, h.to)}` };
    if (h.from !== null && h.to !== null && h.to <= h.from)
      return {
        ruim: focada !== h.chave || marcaIncompletos,
        texto:
          'O "Até" precisa ser depois do "De". Para passar da meia-noite, use dois horários: um até 23:59 e outro a partir de 00:00, no dia seguinte.',
      };
    const semDia = !h.days.length;
    const semHora = h.from === null || h.to === null;
    return {
      ruim: marcaIncompletos,
      texto:
        semDia && semHora
          ? 'Escolha os dias e o horário.'
          : semDia
            ? 'Escolha ao menos um dia.'
            : 'Falta o horário.',
    };
  }
</script>

<section class="horarios" aria-label="os horários do alerta">
  <div class="titulo"><Icone nome="relogio" tamanho={16} traco={2.2} />Horários</div>
  <label class="check">
    <input
      type="checkbox"
      checked={limita}
      onchange={(e) => {
        limita = e.currentTarget.checked;
        onmuda();
      }}
    />
    Limitar o alerta a alguns horários
  </label>

  {#if !limita}
    <small class="muted">O alerta vale a qualquer hora.</small>
  {:else}
    <small class="muted fuso">No fuso do servidor{fuso ? `: ${fuso}` : ''}.</small>

    <GradeDosHorarios cobertura={coberturaDaLista} {realce} onpinta={pinta} />

    <div class="titulo menor">
      Os horários{#if horarios.length}<span class="muted">({horarios.length})</span>{/if}
    </div>
    <div class="lista" class:refeita bind:this={lista}>
      {#each horarios as h (h.chave)}
        {@const n = nota(h)}
        <div
          class="horario"
          class:incompleto={n.ruim}
          role="group"
          aria-label="horário"
          onpointerenter={() => (realcado = h.chave)}
          onpointerleave={() => (realcado = null)}
          onfocusin={() => {
            realcado = h.chave;
            focada = h.chave;
          }}
          onfocusout={() => {
            realcado = null;
            focada = null;
          }}
        >
          <div class="dias" role="group" aria-label="os dias">
            {#each DIAS as nome, d (d)}
              <button
                type="button"
                class="dia"
                title={DIAS_LONGOS[d]}
                aria-pressed={h.days.includes(d)}
                onclick={() => alternaDia(h, d)}>{nome}</button
              >
            {/each}
          </div>
          <div class="horas">
            <CampoHora
              rotulo="De"
              valor={h.from}
              onmuda={(m: number | null) => troca(h.chave, { from: m })}
            />
            <CampoHora
              rotulo="Até"
              valor={h.to}
              onmuda={(m: number | null) => troca(h.chave, { to: m })}
            />
            <button
              type="button"
              class="ghost remover"
              title="remover este horário"
              aria-label="remover este horário"
              onclick={() => remove(h.chave)}
            >
              <Icone nome="fechar" tamanho={18} traco={2.4} />
            </button>
          </div>
          <div class="nota" class:ruim={n.ruim}>{n.texto}</div>
        </div>
      {:else}
        <p class="muted small vazia">Nenhum horário ainda: marque na grade, ou digite um novo.</p>
      {/each}
    </div>

    <div class="acoes">
      <button
        type="button"
        class="mais"
        disabled={horarios.length >= MAX_HORARIOS}
        onclick={() => void adiciona(novoHorario())}
      >
        <Icone nome="mais" tamanho={16} traco={2.4} />novo horário
      </button>
      <span class="espaco"></span>
      <button
        type="button"
        class="ghost atalho"
        title="dois horários: todo dia das 22:00 às 23:59 e das 00:00 às 05:59"
        disabled={horarios.length + 2 > MAX_HORARIOS}
        onclick={() =>
          void adiciona(
            novoHorario([...TODO_DIA], 22 * 60, DIA - 1),
            novoHorario([...TODO_DIA], 0, 6 * 60 - 1),
          )}>+ toda noite</button
      >
      <button
        type="button"
        class="ghost atalho"
        title="sábado e domingo, o dia inteiro"
        disabled={horarios.length >= MAX_HORARIOS}
        onclick={() => void adiciona(novoHorario([0, 6], 0, DIA - 1))}>+ fim de semana</button
      >
    </div>
  {/if}
</section>

<style>
  .horarios {
    display: grid;
    gap: 12px;
    padding-top: 14px;
    border-top: 1px solid var(--line);
  }
  .titulo {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 600;
    font-size: 14px;
  }
  .titulo.menor {
    gap: 6px;
    font-size: 13px;
    margin-bottom: -4px;
  }
  .titulo .muted {
    font-weight: 400;
  }
  .fuso {
    margin: -4px 0 0;
  }

  label.check {
    display: flex;
    align-items: center;
    gap: 9px;
    color: var(--fg);
    font-size: 15px;
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

  .lista {
    display: grid;
    gap: 8px;
  }
  .lista.refeita .horario {
    animation: refeita 0.9s ease-out;
  }
  @keyframes refeita {
    from {
      border-color: var(--accent);
      background: var(--ativo-fundo);
    }
  }
  .vazia {
    margin: 0;
  }
  .horario {
    display: grid;
    gap: 8px;
    padding: 10px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg);
  }
  .horario.incompleto {
    border-color: color-mix(in srgb, var(--bad) 45%, var(--line));
  }
  .dias {
    display: grid;
    grid-template-columns: repeat(7, minmax(0, 1fr));
    gap: 4px;
  }
  .dia {
    min-height: 36px;
    padding: 0;
    font-size: 13px;
    color: var(--dim);
    border-radius: 6px;
  }
  .dia[aria-pressed='true'] {
    color: var(--fg);
    background: var(--ativo-fundo);
    border-color: var(--accent);
  }
  .horas {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 44px;
    gap: 8px;
    align-items: end;
  }
  .remover {
    padding: 0;
    min-height: 44px;
    display: grid;
    place-items: center;
    color: var(--dim);
  }
  .nota {
    margin-top: -2px;
    font-size: 12px;
    color: var(--dim);
  }
  .nota.ruim {
    color: var(--bad);
  }

  .acoes {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
  }
  .mais {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .espaco {
    flex: 1;
  }
  .atalho {
    min-height: 36px;
    padding: 4px 11px;
    font-size: 13px;
    border-radius: 999px;
    color: var(--dim);
  }
</style>
