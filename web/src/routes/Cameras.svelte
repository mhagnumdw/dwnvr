<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import {
    cameras,
    loadCameras,
    loadHealth,
    health,
    pollHealth,
    HEALTH_POLL_MS,
  } from '../lib/state.svelte.ts';
  import { api, mensagemDe } from '../lib/api.ts';
  import type { AreaDaCamera, Camera, CameraCadastrada, Orfa, Stream } from '../lib/api.ts';
  import { paramsAtuais, escrever } from '../lib/rota.svelte.ts';
  import {
    hhmm,
    dias,
    kbps,
    bytes,
    bytesDeMB,
    resolucao,
    ddmm,
    duracao,
    gravadoSeHouverBuraco,
  } from '../lib/format.ts';
  import { AJUDA_RETIDO, AJUDA_CABEM, AJUDA_SENSIBILIDADE, NIVEIS, nivel } from '../lib/ajudas.ts';
  import Modal from '../components/Modal.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import SemCameras from '../components/SemCameras.svelte';
  import Icone from '../components/Icone.svelte';
  import Abas from '../components/Abas.svelte';
  import MiniaturaDasAreas from '../components/MiniaturaDasAreas.svelte';
  import type EditorDeAreaT from '../components/EditorDeArea.svelte';
  import { REGRAS, REGRA_PADRAO, MAX_AREAS, pontosDoCadastro, pontosSVG } from '../lib/area.ts';
  import type { Ponto, Regra } from '../lib/area.ts';
  import { quadroCheio, ultimoSegmento } from '../lib/thumbs.ts';

  // A câmera no formulário, com o que a tela sabe do áudio dela. Os campos com
  // `_` não vão para a API.
  type Edicao = CameraCadastrada & {
    _novo: boolean;
    _hasAudio: boolean;
    _audioCodecs: string[];
    _sondando: boolean;
    _sondaErro: string;
  };

  // De quem são as gravações a apagar: uma câmera cadastrada ou uma órfã.
  interface Alvo {
    id: string;
    nome: string;
    bytes: number;
    orfa: boolean;
  }

  let editing = $state<Edicao | null>(null); // cópia da câmera em edição, ou null
  let removendo = $state<CameraCadastrada | null>(null); // câmera aguardando confirmação de remoção
  let apagando = $state<Alvo | null>(null); // gravações aguardando confirmação de apagamento
  let apagarGravacoes = $state(false); // checkbox do diálogo de remoção
  let saving = $state(false);
  let error = $state('');

  // As abas do formulário. Ele sempre abre na primeira: é onde estão o nome e a
  // cota, o que se procura ao cadastrar. A de Áreas só existe com a marcação de
  // movimento ligada e o detector de objetos configurado: sem os dois a câmera
  // não tem detecção, e a área não teria o que filtrar. Desligar a marcação
  // esconde a aba, mas não apaga as áreas.
  const comAreas = $derived(!!editing?.detect && !!cameras.detector);
  const ABAS = $derived([
    { id: 'gravacao', rotulo: 'Gravação' },
    { id: 'deteccao', rotulo: 'Detecção' },
    ...(comAreas
      ? [
          {
            id: 'areas',
            rotulo: editing?.areas?.length ? `Áreas (${editing.areas.length})` : 'Áreas',
          },
        ]
      : []),
  ]);
  let aba = $state('gravacao');
  // No cadastro, o rodapé guia aba a aba e o "salvar" só aparece na última:
  // quem cadastra passa por tudo. Na edição se vai direto a uma coisa, e as
  // abas bastam.
  const indiceDaAba = $derived(ABAS.findIndex((a) => a.id === aba));
  const guiado = $derived(!!editing?._novo && indiceDaAba < ABAS.length - 1);
  // Depois da troca, o foco vai para o painel novo: o de antes fica `inert` e
  // o botão clicado pode sumir do rodapé, e sem isto o foco cai no <body> e o
  // Tab recomeça do topo. Avançando, vai ao primeiro campo, e quem digitava no
  // Enter segue digitando. Voltando, vai à aba: no celular, o foco no campo de
  // texto abriria o teclado sem ninguém pedir.
  async function irPara(passo: number) {
    aba = ABAS[indiceDaAba + passo]?.id ?? aba;
    await tick();
    const alvo =
      passo > 0
        ? document
            .getElementById(`cam-painel-${aba}`)
            ?.querySelector<HTMLElement>('input, select, textarea, button')
        : document.getElementById(`cam-aba-${aba}`);
    alvo?.focus();
  }

  const novos = $derived(cameras.streams.filter((s) => !s.registered));
  const faixas = $derived(cameras.faixas);

  // Reinício do go2rtc: ele só lê o go2rtc.yaml quando sobe.
  let confirmandoReinicio = $state(false);
  let reiniciando = $state(false);
  // O go2rtc responde ao pedido antes de reiniciar: perguntar logo em seguida
  // ainda pega o processo velho, com a lista velha.
  const REINICIO_PRIMEIRA_PERGUNTA_MS = 1500;
  const REINICIO_INTERVALO_MS = 1000;
  // Medido no go2rtc local: volta em menos de 2 s. Passado disto, desiste e
  // deixa a tela mostrar o erro de conexão, que é a verdade.
  const REINICIO_DESISTE_MS = 20000;

  const cadastradas = $derived(new Set(cameras.list.map((c) => c.id)));
  // Câmera cadastrada que saiu do arquivo para de gravar depois do reinício: é o
  // único efeito do reinício que alguém pode não querer, e o diálogo o repete.
  const vaoParar = $derived(
    (cameras.go2rtcConfigFile?.foraDoArquivo ?? []).filter((id) => cadastradas.has(id)),
  );

  // Enquanto a sonda não responde, as opções de áudio seguem bloqueadas; se ela
  // FALHAR, elas são liberadas. Não conseguir verificar não é o mesmo que saber
  // que não tem áudio, e bloquear nesse caso impediria de configurar uma câmera
  // boa que só estava fora do ar na hora do cadastro.
  const audioBloqueado = $derived(!editing?._hasAudio && !editing?._sondaErro);

  // `#cams?editar=<id>` abre direto o formulário da câmera: é o link da
  // tela Diagnóstico para quem está com a detecção desligada. O parâmetro é
  // consumido ao abrir, senão recarregar a página reabriria o formulário.
  onMount(async () => {
    const id = paramsAtuais().get('editar');
    if (!cameras.list.length) await loadCameras();
    if (!id) return;
    escrever({ editar: null });
    const cam = cameras.list.find((c) => c.id === id);
    if (cam) editar(cam);
  });

  // O caminho comum para câmera nova é trocar de janela, editar o go2rtc.yaml e
  // voltar: recarregar quando a aba reaparece poupa o clique no botão. Com a aba
  // escondida não há custo nenhum - o evento só dispara na troca.
  function aoVoltarParaAba() {
    if (!document.hidden) void loadCameras();
  }
  onMount(() => {
    document.addEventListener('visibilitychange', aoVoltarParaAba);
    return () => {
      document.removeEventListener('visibilitychange', aoVoltarParaAba);
    };
  });

  const stop = pollHealth();
  onDestroy(stop);

  function statusOf(id: string) {
    return health.cameras.find((h) => h.id === id);
  }

  // estimaDias responde "com esta cota, quantos dias de gravação cabem?" - e o
  // servidor já responde isso em retainDays, na densidade do que está gravado.
  //
  // Aqui só falta o "e se eu puser 40 GB?" do formulário de edição, e para isso
  // basta escalar: a estimativa é linear na cota (dias = cota / consumo por
  // dia), então a razão entre a cota digitada e a gravada dá o número novo sem
  // repetir no JS a conta que o Go faz. O denominador é o st.quotaMB, que é
  // sempre a cota salva - cam.quotaMB é o que está no formulário e se move
  // enquanto se digita.
  function estimaDias(cam: Camera, quotaMB = cam.quotaMB) {
    const st = statusOf(cam.id);
    if (!st?.retainDays || !st.quotaMB || quotaMB === undefined) return undefined;
    return (st.retainDays * quotaMB) / st.quotaMB;
  }

  function novo(streamName: string) {
    const s = cameras.streams.find((x) => x.name === streamName);
    editing = {
      id: streamName,
      name: streamName.replace(/^cam[_-]?/, '').replace(/^./, (c) => c.toUpperCase()),
      enabled: true,
      audio: 'none',
      quotaMB: 10240,
      segmentSeconds: 30,
      maxDays: 0,
      // Os três vêm dos defaults do servidor, onde a detecção nasce DESLIGADA:
      // quem só grava não paga nada por ela. O literal só cobre resposta sem
      // "padrao".
      detect: cameras.padrao?.detect ?? false,
      detectMecanismo: cameras.padrao?.detectMecanismo ?? 'kleinberg-p',
      detectSensibilidade: cameras.padrao?.detectSensibilidade ?? 5,
      _novo: true,
      ...audioConhecido(s),
    };
    aba = 'gravacao';
    void sondarAudio(streamName);
  }

  function editar(cam: CameraCadastrada) {
    const s = cameras.streams.find((x) => x.name === cam.id);
    editing = { ...cam, _novo: false, ...audioConhecido(s) };
    aba = 'gravacao';
    void sondarAudio(cam.id);
  }

  // O que já se sabe do áudio quando o formulário abre. Num stream ocioso isso
  // é sempre "não tem", e é mentira: o go2rtc só lista as trilhas enquanto
  // alguém consome o stream. Por isso sondarAudio() vem logo atrás.
  const audioConhecido = (s: Stream | undefined) => ({
    _hasAudio: s?.hasAudio ?? false,
    _audioCodecs: s?.audioCodecs ?? [],
    _sondando: false,
    _sondaErro: '',
  });

  // Pergunta ao servidor se a câmera entrega áudio. Só vale a pena quando a
  // resposta que já se tem é "não": um "sim" veio do medias e não muda.
  //
  // A resposta pode demorar alguns segundos (o servidor abre o stream para
  // descobrir), então o formulário já está na tela quando ela chega - daí a
  // conferência de que ele ainda é o mesmo, que descarta a resposta de um
  // formulário já fechado ou trocado por outro.
  async function sondarAudio(streamName: string) {
    const form = editing;
    if (!form || form._hasAudio) return;
    form._sondando = true;
    try {
      const r = await api.probeStream(streamName);
      if (editing !== form) return;
      form._hasAudio = r.hasAudio;
      form._audioCodecs = r.audioCodecs ?? [];
      form._sondaErro = r.erro ?? '';
    } catch (e) {
      if (editing !== form) return;
      form._sondaErro = mensagemDe(e);
    } finally {
      if (editing === form) form._sondando = false;
    }
  }

  // Campo inválido numa aba escondida: o navegador recusa o envio, mas não tem
  // como mostrar o balão num campo invisível, e o "salvar" parece não fazer
  // nada. O primeiro campo inválido traz a aba dele para a frente e mostra o
  // motivo; os seguintes do mesmo envio ficam para o próximo clique.
  async function campoInvalido(e: Event) {
    const campo = e.target as HTMLInputElement;
    const painel = campo.closest<HTMLElement>('[role="tabpanel"]');
    const dono = painel?.dataset['aba'];
    if (!dono || dono === aba) return;
    aba = dono;
    await tick();
    campo.reportValidity();
  }

  // --- a aba Áreas ----------------------------------------------------------

  // O quadro de fundo das áreas: o keyframe do último segmento, decodificado
  // aqui na resolução da câmera. Só é buscado quando a aba abre, e sai da
  // memória quando o formulário fecha.
  let quadro = $state<{
    cam: string;
    t: number | null;
    url: string;
    proporcao: number;
    buscando: boolean;
    erro: string;
  } | null>(null);
  // Cada busca tem o seu número: a resposta de uma busca já trocada por outra,
  // ou de um formulário já fechado, é descartada.
  let busca = 0;

  function soltaQuadro() {
    busca++;
    if (quadro?.url) URL.revokeObjectURL(quadro.url);
    quadro = null;
  }

  async function urlDoQuadro(bmp: ImageBitmap) {
    const c = document.createElement('canvas');
    c.width = bmp.width;
    c.height = bmp.height;
    c.getContext('2d')?.drawImage(bmp, 0, 0);
    const blob = await new Promise<Blob | null>((r) => {
      c.toBlob(r, 'image/jpeg', 0.9);
    });
    if (!blob) throw new Error('não deu para guardar o quadro');
    return URL.createObjectURL(blob);
  }

  async function buscaQuadro(cam: string, novo: boolean) {
    const minha = ++busca;
    const antes = quadro?.cam === cam ? quadro : null;
    if (!antes && quadro?.url) URL.revokeObjectURL(quadro.url);
    quadro = {
      cam,
      t: antes?.t ?? null,
      url: antes?.url ?? '',
      proporcao: antes?.proporcao ?? 16 / 9,
      buscando: true,
      erro: '',
    };
    const q = quadro;
    // A câmera nova ainda não gravou, e o servidor nem a conhece.
    if (novo) {
      q.buscando = false;
      q.erro =
        'Esta câmera ainda não gravou nada. Salve, e depois do primeiro minuto de gravação volte aqui, em editar, na aba Áreas, para desenhar sobre a imagem dela.';
      return;
    }
    try {
      const t = await ultimoSegmento(cam);
      if (t === null) throw new Error('esta câmera ainda não tem gravação para desenhar em cima');
      const bmp = await quadroCheio(cam, t);
      const proporcao = bmp.width / bmp.height;
      const url = await urlDoQuadro(bmp).finally(() => {
        bmp.close();
      });
      if (minha !== busca) {
        URL.revokeObjectURL(url);
        return;
      }
      if (q.url) URL.revokeObjectURL(q.url);
      q.t = t;
      q.url = url;
      q.proporcao = proporcao;
    } catch (e) {
      if (minha === busca) q.erro = mensagemDe(e);
    } finally {
      if (minha === busca) q.buscando = false;
    }
  }

  $effect(() => {
    const cam = editing?.id;
    if (aba === 'areas' && cam && quadro?.cam !== cam) void buscaQuadro(cam, !!editing?._novo);
  });
  $effect(() => {
    if (!editing) soltaQuadro();
  });
  onDestroy(soltaQuadro);

  const legendaDoQuadro = (t: number | null) =>
    t === null ? '' : `quadro de ${ddmm(t)}, ${hhmm(t)}`;

  // O editor vem num chunk à parte, como em Detecções.
  let EditorDeArea = $state<typeof EditorDeAreaT | null>(null);
  // A área aberta no editor: o índice dela, ou null para uma nova.
  let areaAberta = $state<{ indice: number | null } | null>(null);

  function abreArea(indice: number | null) {
    if (!EditorDeArea)
      void import('../components/EditorDeArea.svelte').then((m) => {
        EditorDeArea = m.default;
      });
    areaAberta = { indice };
  }

  function aplicaArea(pontos: Ponto[], regra: Regra, nome: string) {
    if (!editing || !areaAberta) return;
    const areas = editing.areas ?? [];
    const i = areaAberta.indice;
    const nova: AreaDaCamera = {
      ...(i === null ? {} : areas[i]),
      name: nome,
      rule: regra,
      points: pontosDoCadastro(pontos),
    };
    editing.areas = i === null ? [...areas, nova] : areas.map((a, j) => (j === i ? nova : a));
    areaAberta = null;
  }

  function apagaArea() {
    if (!editing || areaAberta?.indice == null) return;
    const i = areaAberta.indice;
    editing.areas = (editing.areas ?? []).filter((_, j) => j !== i);
    areaAberta = null;
  }

  async function salvar() {
    if (!editing) return;
    saving = true;
    error = '';
    try {
      const { _novo, _hasAudio, _audioCodecs, _sondando, _sondaErro, ...cam } = editing;
      await api.saveCamera(cam);
      await loadCameras();
      editing = null;
    } catch (e) {
      error = mensagemDe(e);
    } finally {
      saving = false;
    }
  }

  // O tamanho vem da medição do diagnóstico, que é a mesma fonte do chip de
  // disco do card: dizer "apagar 3,2 GB" só ajuda se o número for o que a
  // pessoa já estava vendo.
  function tamanhoDe(cam: Camera) {
    return statusOf(cam.id)?.diskBytes ?? 0;
  }

  // Um alvo normalizado para que um único diálogo sirva os dois casos: a câmera
  // cadastrada, que tem nome e status, e a órfã, que só tem o que a varredura do
  // disco descobriu.
  const alvoDaCamera = (cam: CameraCadastrada): Alvo => ({
    id: cam.id,
    nome: cam.name,
    bytes: tamanhoDe(cam),
    orfa: false,
  });

  const alvoDaOrfa = (o: Orfa): Alvo => ({ id: o.id, nome: o.id, bytes: o.bytes, orfa: true });

  function abrirRemocao(cam: CameraCadastrada) {
    // Sempre desmarcado ao abrir: preservar as gravações é o padrão, e herdar a
    // marcação de uma remoção anterior apagaria vídeo sem ninguém pedir.
    apagarGravacoes = false;
    removendo = cam;
  }

  async function confirmarRemocao() {
    const cam = removendo;
    if (!cam) return;
    const comGravacoes = apagarGravacoes;
    removendo = null;
    error = '';
    try {
      await api.deleteCamera(cam.id, { recordings: comGravacoes });
      await loadCameras();
      await loadHealth();
    } catch (e) {
      error = mensagemDe(e);
    }
  }

  const espera = (ms: number) => new Promise((r) => setTimeout(r, ms));

  async function reiniciarGo2rtc() {
    confirmandoReinicio = false;
    reiniciando = true;
    error = '';
    try {
      await api.reiniciarGo2rtc();
      // Pergunta direto à API, sem passar pelo estado: enquanto o go2rtc está
      // fora, a resposta traz go2rtcError, e jogá-la na tela piscaria o aviso
      // de "não foi possível falar com o go2rtc" no meio de um reinício normal.
      const desiste = Date.now() + REINICIO_DESISTE_MS;
      await espera(REINICIO_PRIMEIRA_PERGUNTA_MS);
      while (Date.now() < desiste) {
        const data = await api.cameras().catch(() => null);
        if (data && !data.go2rtcError) break;
        await espera(REINICIO_INTERVALO_MS);
      }
    } catch (e) {
      error = mensagemDe(e);
    } finally {
      await loadCameras();
      await loadHealth();
      reiniciando = false;
    }
  }

  async function confirmarApagamento() {
    const alvo = apagando;
    if (!alvo) return;
    apagando = null;
    error = '';
    try {
      await api.deleteRecordings(alvo.id);
      // As duas: loadCameras refaz a lista de órfãs, loadHealth refaz o chip de
      // disco. Sem a segunda, o card seguiria mostrando os bytes que já foram.
      await loadCameras();
      await loadHealth();
    } catch (e) {
      error = mensagemDe(e);
    }
  }
</script>

<div class="page">
  {#if cameras.go2rtcError}
    <p class="card warnbox small">
      Não foi possível falar com o go2rtc: {cameras.go2rtcError}<br />
      As câmeras já cadastradas continuam gravando, mas não dá para descobrir novas.
    </p>
  {/if}

  {#if error}<p class="card errbox small">{error}</p>{/if}

  <div class="list">
    {#each cameras.list as cam (cam.id)}
      {@const st = statusOf(cam.id)}
      <div class="card cam">
        <div class="row wrap head">
          <span class="dot" class:ok={st?.connected} class:bad={cam.enabled && st && !st.connected}
          ></span>
          <strong>{cam.name}</strong>
          <code class="muted small">{cam.id}</code>
          <span class="spacer"></span>
          <!-- Os três num grupo próprio para quebrarem juntos: soltos na linha,
               em tela de celular o "remover" descia sozinho para a esquerda,
               longe dos outros dois e do canto onde o polegar espera achá-lo. -->
          <div class="row acoes">
            <button class="ghost small" onclick={() => editar(cam)}>editar</button>
            <!-- Rótulo por extenso: só "gravações" se leria como "ver as gravações". -->
            <button class="ghost small danger" onclick={() => (apagando = alvoDaCamera(cam))}>
              apagar gravações
            </button>
            <button class="ghost small danger" onclick={() => abrirRemocao(cam)}>remover</button>
          </div>
        </div>

        <div class="row wrap chips">
          {#if !cam.enabled}<span class="chip">desabilitada</span>{/if}
          <span class="chip">{st?.videoCodec ?? '-'}</span>
          <span class="chip">{resolucao(st?.width, st?.height)}</span>
          <span class="chip">áudio: {cam.audio}</span>
          {#if cam.detect}
            <span class="chip" title={AJUDA_SENSIBILIDADE}>
              movimento: {nivel(cam.detectSensibilidade)?.rotulo ?? cam.detectSensibilidade}
            </span>
          {/if}
          <span class="chip">{kbps(st?.bitrateKbps)}</span>
          <span class="chip">{bytes(st?.diskBytes ?? 0)} de {bytesDeMB(cam.quotaMB)}</span>
          <!-- O par "retido / cabem" é o que torna a cota compreensível: o
               primeiro é o passado que existe, o segundo é o que ela ainda
               comporta. Sozinho, cada um deles engana.
               A explicação é a mesma da coluna equivalente no Diagnóstico, e
               vem de ajudas.ts justamente para não divergir dela. O "Desde"
               entra só aqui e na célula de lá: é por câmera, ao contrário do
               resto do texto, e não caberia no chip sem empurrar os outros. -->
          {#if st?.oldestSegmentAt}
            {@const desde = new Date(st.oldestSegmentAt)}
            {@const retido = Date.now() - desde.getTime()}
            {@const gravado = gravadoSeHouverBuraco(retido, st.recordedMs)}
            <span class="chip retido" title="{AJUDA_RETIDO} Desde {ddmm(desde.getTime())}">
              retido: {duracao(retido)}{#if gravado}
                · gravado {duracao(gravado)}{/if}
            </span>
          {/if}
          {#if st?.retainDays}
            <span class="chip retain" title={AJUDA_CABEM}>cabem ≈ {dias(st.retainDays)}</span>
          {/if}
        </div>

        <!-- As áreas só mostram: quem muda é o "editar". Com a mesma condição
             da aba, para o card não falar de áreas que não filtram nada. -->
        {#if cam.areas?.length && cam.detect && cameras.detector}
          <div class="row wrap chips">
            <span class="chip areas-chip">
              <MiniaturaDasAreas cam={cam.id} areas={cam.areas} />
              {cam.areas.length === 1 ? '1 área' : `${cam.areas.length} áreas`}
            </span>
          </div>
        {/if}

        <!-- Onde os arquivos estão de verdade. Toda vez que a pergunta sai do
             navegador - fazer backup, olhar o disco por ssh, entender de onde
             vieram os bytes do chip de uso - é este caminho que se procura, e
             ele não estava escrito em lugar nenhum da interface. Linha própria
             porque é o único chip que pode ficar comprido: junto dos outros ele
             empurraria a retenção para fora do alinhamento a cada nome de pasta. -->
        {#if cam.dir}
          <div class="row wrap chips">
            <span class="chip storage" title="pasta das gravações desta câmera"
              >storage: {cam.dir}</span
            >
          </div>
        {/if}

        {#if st?.lastError && !st.connected}
          <p class="small err">{st.lastError}</p>
        {/if}
      </div>
    {/each}

    {#if !cameras.list.length && !cameras.loading}
      <SemCameras link={false} />
    {/if}
  </div>

  <!-- Sempre visível, mesmo vazio: enquanto este bloco só aparecia quando havia
       stream novo, quem chegava na tela com tudo já cadastrado não encontrava o
       botão de adicionar e concluía que ele não existia. -->
  <div class="card ajuda">
    <div class="row titulo">
      <h3>Disponíveis no go2rtc</h3>
      <button
        class="ghost atualizar"
        class:girando={cameras.loading}
        onclick={loadCameras}
        disabled={cameras.loading}
        aria-label="atualizar"
        title="perguntar de novo ao go2rtc quais streams ele serve"
      >
        <Icone nome="atualizar" tamanho={16} traco={2.4} />
      </button>
    </div>

    {#if cameras.go2rtcConfigFile}
      {@const arq = cameras.go2rtcConfigFile}
      <div class="card warnbox aviso">
        <p class="small">
          O <code>go2rtc.yaml</code> mudou depois que o go2rtc subiu. Ele só lê o arquivo quando sobe,
          então estas mudanças ainda não valem:
        </p>
        <ul class="small">
          {#each arq.novas ?? [] as id (id)}
            <li>
              <code>{id}</code>
              <span class="muted">nova - aparece para cadastrar depois do reinício</span>
            </li>
          {/each}
          {#each arq.alteradas ?? [] as id (id)}
            <li><code>{id}</code> <span class="muted">configuração mudou</span></li>
          {/each}
          {#each arq.foraDoArquivo ?? [] as id (id)}
            <li>
              <code>{id}</code>
              <span class="muted">
                {cadastradas.has(id)
                  ? 'saiu do arquivo - para de gravar depois do reinício'
                  : 'saiu do arquivo - some desta lista depois do reinício'}
              </span>
            </li>
          {/each}
        </ul>
        <div class="row acao">
          <button class="warn" onclick={() => (confirmandoReinicio = true)} disabled={reiniciando}>
            {reiniciando ? 'reiniciando…' : 'reiniciar go2rtc'}
          </button>
        </div>
      </div>
    {/if}

    {#if novos.length}
      <div class="row wrap">
        {#each novos as s (s.name)}
          <button title="Clique para adicionar" onclick={() => novo(s.name)}>
            + {s.name}
            {#if s.hasAudio}<span class="chip">áudio</span>{/if}
            {#if s.transcoding}<span class="chip warn">ffmpeg</span>{/if}
          </button>
        {/each}
      </div>
    {:else if cameras.go2rtcError}
      <p class="muted small vazio">
        Não dá para listar os streams enquanto o go2rtc não responder.
      </p>
    {:else}
      <p class="muted small vazio">
        Nenhum stream novo - o dwnvr já grava todos os que o go2rtc serve.
      </p>
    {/if}

    <!-- Depois da lista: quem já entendeu como funciona quer o botão primeiro, e
         quem não entendeu lê a explicação ao lado do que ela explica. -->
    <p class="muted small explica">
      Câmera não se cadastra aqui do zero: o dwnvr grava o que o go2rtc entrega e não guarda
      endereço nem senha de câmera. Declare o stream no <code>go2rtc.yaml</code> e reinicie o go2rtc,
      que só lê o arquivo quando sobe. Aí ele aparece nesta lista - um clique escolhe cota, áudio e retenção,
      e a gravação começa. Vale também para mudar uma câmera que já grava.
    </p>

    <!-- Sem aviso não quer dizer arquivo igual: mudar a fonte ffmpeg de uma
         câmera que está gravando não é detectável (ver go2rtc.Divergencias).
         O reinício fica ao alcance, discreto. -->
    {#if !cameras.go2rtcConfigFile && !cameras.go2rtcError}
      <p class="muted small reinicio">
        Mudou o <code>go2rtc.yaml</code> e não apareceu aqui?
        <button class="link" onclick={() => (confirmandoReinicio = true)} disabled={reiniciando}>
          {reiniciando ? 'reiniciando…' : 'reiniciar o go2rtc'}
        </button>
      </p>
    {/if}
  </div>

  <!-- Só aparece quando há algo: uma seção vazia permanente sugeriria que sobrar
       gravação órfã é o estado normal, e não é. -->
  {#if cameras.orphans.length}
    <div class="card orfas">
      <h3>Gravações de câmeras removidas</h3>
      <p class="muted small">
        Não contam na cota de ninguém, não abrem na tela de Gravações e a retenção não as alcança,
        nem com o disco cheio - só saem do disco por aqui.
      </p>

      {#each cameras.orphans as o (o.id)}
        <!-- A linha não quebra; quem quebra é o bloco de dados. Assim o botão
             fica sempre no mesmo canto, em vez de descer para a esquerda quando
             os chips não cabem. -->
        <div class="row orfa">
          <div class="row wrap dados">
            <code>{o.id}</code>
            {#if o.days}
              <span class="chip">{bytes(o.bytes)}</span>
              <span class="chip">
                {ddmm(o.firstMs)} a {ddmm(o.lastMs)} · {o.days}
                {o.days > 1 ? 'dias' : 'dia'}
              </span>
            {:else}
              <!-- Diretório sem índice: sobra de uma evicção ou de uma cópia à
                   mão. Não dá para dizer o tamanho, mas esconder seria pior. -->
              <span class="chip warn">sem índice</span>
            {/if}
          </div>
          <button class="ghost small danger" onclick={() => (apagando = alvoDaOrfa(o))}>
            apagar
          </button>
        </div>
      {/each}
    </div>
  {/if}

  <!-- O separador vai como expressão porque o Svelte apara o espaço no início
       de um bloco {#if}, e sem isso sai "5s· última leitura". -->
  <p class="muted small">
    Atualizado a cada {HEALTH_POLL_MS / 1000}s{#if health.updatedAt}{' · '}última leitura há {duracao(
        Date.now() - health.updatedAt,
      )}{/if}
  </p>
</div>

{#if editing}
  <!-- O Escape do editor de área fecha só o editor: o formulário de baixo
       fica. -->
  <Modal
    onclose={() => {
      if (!areaAberta) editing = null;
    }}
  >
    <!-- No cadastro, antes da última aba, o envio é o "próximo": o Enter num
         campo avança, e a validação do navegador barra o campo vazio antes de
         sair da aba dele. -->
    <form
      class="fields"
      onsubmit={(e) => {
        e.preventDefault();
        void (guiado ? irPara(1) : salvar());
      }}
      oninvalidcapture={campoInvalido}
    >
      <h3>{editing._novo ? 'Cadastrar' : 'Editar'} {editing.id}</h3>

      <Abas abas={ABAS} bind:ativa={aba} prefixo="cam" />

      <!-- Os painéis ficam um em cima do outro na mesma célula do grid, e o
           escondido só some da vista: o formulário fica com a altura do maior
           e não pula ao trocar de aba, e os campos dele continuam no envio. -->
      <div class="paineis">
        <div
          class="fields"
          role="tabpanel"
          id="cam-painel-gravacao"
          aria-labelledby="cam-aba-gravacao"
          data-aba="gravacao"
          inert={aba !== 'gravacao'}
        >
          <label>
            Nome
            <input bind:value={editing.name} required />
          </label>

          <label class="check">
            <input type="checkbox" bind:checked={editing.enabled} />
            Gravar esta câmera
          </label>

          <label>
            Áudio
            <select bind:value={editing.audio}>
              <option value="none">nenhum - custo zero</option>
              <option value="flac" disabled={audioBloqueado}>
                FLAC - ~0,6% de CPU, +260 kbps de disco
              </option>
              <option value="aac" disabled={audioBloqueado}>
                AAC - ~10% de CPU, +64 kbps (exige fonte ffmpeg no go2rtc)
              </option>
            </select>
            {#if editing._sondando}
              <small class="muted">Verificando se esta câmera entrega áudio...</small>
            {:else if editing._sondaErro}
              <small class="muted">
                Não deu para verificar o áudio desta câmera ({editing._sondaErro}). As opções estão
                liberadas; o Diagnóstico avisa se o áudio não chegar.
              </small>
            {:else if !editing._hasAudio}
              <small class="muted">
                Esta câmera não entrega trilha de áudio. Remova o <code>#media=video</code>
                da fonte no go2rtc.yaml para habilitá-la.
              </small>
            {:else if editing._audioCodecs.length}
              <small class="muted">
                Esta câmera entrega {editing._audioCodecs.join(', ')}.
              </small>
            {/if}
          </label>

          <label>
            Cota em disco (MB)
            <!-- step=1 porque no input nativo o step também é a régua de validação:
                 com step=100 o browser recusava 20480 (20 GiB), que é exatamente o
                 tipo de número que se digita numa cota. O min vem da API: sem
                 resposta, o input fica sem ele e quem recusa é o servidor. -->
            <input
              type="number"
              bind:value={editing.quotaMB}
              min={faixas?.quotaMB?.min}
              step="1"
              required
            />
            <!-- O campo é em MB porque é assim que a cota é guardada, mas quem digita
                 20480 quer saber que isso são 20 GB. -->
            <small class="muted">
              = {bytesDeMB(editing.quotaMB)} ·
              {#if estimaDias(editing, editing.quotaMB)}
                ≈ {dias(estimaDias(editing, editing.quotaMB))} no consumo médio medido
              {:else}
                a estimativa aparece após a primeira medição de taxa
              {/if}
            </small>
          </label>

          <label>
            Duração do segmento (s)
            <input
              type="number"
              bind:value={editing.segmentSeconds}
              min={faixas?.segmentSeconds?.min}
              max={faixas?.segmentSeconds?.max}
              step="10"
            />
            <small class="muted">o corte real espera o próximo keyframe</small>
          </label>
        </div>

        <div
          class="fields"
          role="tabpanel"
          id="cam-painel-deteccao"
          aria-labelledby="cam-aba-deteccao"
          data-aba="deteccao"
          inert={aba !== 'deteccao'}
        >
          <label class="check">
            <input type="checkbox" bind:checked={editing.detect} />
            Marcar movimento na timeline
          </label>

          {#if editing.detect}
            <label>
              Sensibilidade
              <select bind:value={editing.detectSensibilidade}>
                {#each NIVEIS as n, i (i)}
                  <option value={i + 1}>{i + 1} - {n.rotulo}</option>
                {/each}
              </select>
              <!-- O custo é o que o usuário paga de verdade, e ele é POR CÂMERA:
                   com dez câmeras no nível 4 são 500 olhadas por hora, não 50. -->
              <small class="muted">
                ≈ {nivel(editing.detectSensibilidade)?.porHora} marcas por hora nesta câmera
              </small>
            </label>

            <label>
              Mecanismo
              <select bind:value={editing.detectMecanismo}>
                <option value="kleinberg-p">estatístico (recomendado)</option>
                <option value="periodico">periódico</option>
              </select>
              <small class="muted">
                {editing.detectMecanismo === 'periodico'
                  ? 'dispara em intervalo fixo, ignorando a imagem - custo previsível, mas pega menos'
                  : 'dispara quando a imagem destoa do normal da própria câmera'}
              </small>
            </label>
          {/if}
        </div>

        {#if comAreas}
          {@const areas = editing.areas ?? []}
          <div
            class="fields"
            role="tabpanel"
            id="cam-painel-areas"
            aria-labelledby="cam-aba-areas"
            data-aba="areas"
            inert={aba !== 'areas'}
          >
            <p class="muted small explica-areas">
              Pedaços da imagem com nome, como "Portão". Em Detecções, qualquer pessoa filtra por
              eles. Se a câmera for movida, redesenhe.
            </p>
            {#if quadro?.erro}
              <p class="small aviso-areas">{quadro.erro}</p>
            {/if}
            {#if areas.length}
              <div class="areas">
                {#each areas as a, i (a.id ?? `nova-${i}`)}
                  <div class="area-item">
                    <div class="miniatura" style:aspect-ratio={quadro?.proporcao ?? 16 / 9}>
                      {#if quadro?.url}<img src={quadro.url} alt="" />{/if}
                      <svg viewBox="0 0 1 1" preserveAspectRatio="none" aria-hidden="true">
                        <polygon points={pontosSVG(a.points)} />
                      </svg>
                    </div>
                    <span class="nome-area">
                      <b>{a.name}</b>
                      <span class="muted small">{REGRAS[a.rule ?? REGRA_PADRAO].nome}</span>
                    </span>
                    <button
                      type="button"
                      class="ghost small"
                      onclick={() => abreArea(i)}
                      disabled={!quadro || quadro.buscando}>editar</button
                    >
                  </div>
                {/each}
              </div>
            {/if}
            {#if !editing._novo}
              <button
                type="button"
                class="ghost nova-area"
                onclick={() => abreArea(null)}
                disabled={!quadro?.url || areas.length >= MAX_AREAS}
              >
                <Icone nome="mais" tamanho={16} traco={2.4} />nova área
              </button>
              {#if areas.length >= MAX_AREAS}
                <small class="muted">Até {MAX_AREAS} áreas por câmera.</small>
              {/if}
            {/if}
          </div>
        {/if}
      </div>

      <div class="row">
        {#if editing._novo}
          {#if indiceDaAba > 0}
            <button type="button" class="ghost passo" onclick={() => void irPara(-1)}>
              <Icone nome="chevronEsq" tamanho={16} traco={2.4} />voltar
            </button>
          {/if}
        {:else}
          <button
            type="button"
            class="danger"
            onclick={() => {
              if (editing) abrirRemocao(editing);
              editing = null;
            }}>remover</button
          >
        {/if}
        <span class="spacer"></span>
        <button type="button" class="ghost" onclick={() => (editing = null)}>cancelar</button>
        {#if guiado}
          <button class="primary passo" type="submit">
            próximo<Icone nome="chevronDir" tamanho={16} traco={2.4} />
          </button>
        {:else}
          <button class="primary" type="submit" disabled={saving}>
            {saving ? 'salvando…' : 'salvar'}
          </button>
        {/if}
      </div>
    </form>
  </Modal>

  {#if areaAberta && EditorDeArea}
    {@const areas = editing.areas ?? []}
    {@const i = areaAberta.indice}
    {@const atual = i === null ? null : areas[i]}
    <EditorDeArea
      camera={editing.name}
      titulo={atual ? 'Editar área' : 'Nova área'}
      rotuloAplicar={atual ? 'salvar' : 'criar área'}
      fundo={quadro?.url ? { tipo: 'imagem', url: quadro.url } : null}
      erro={quadro?.erro ?? ''}
      inicial={atual ? { pontos: atual.points, regra: atual.rule ?? REGRA_PADRAO } : null}
      nome={atual?.name ?? ''}
      nomesEmUso={areas.filter((_, j) => j !== i).map((a) => a.name)}
      outras={areas.filter((_, j) => j !== i).map((a) => ({ name: a.name, points: a.points }))}
      quadro={quadro
        ? {
            legenda: legendaDoQuadro(quadro.t),
            atualizando: quadro.buscando,
            onatualizar: () => {
              if (editing) void buscaQuadro(editing.id, false);
            },
          }
        : null}
      onaplicar={aplicaArea}
      onapagar={atual ? apagaArea : null}
      onfechar={() => (areaAberta = null)}
    />
  {/if}
{/if}

{#if removendo}
  {@const tam = tamanhoDe(removendo)}
  <ConfirmDialog
    title="Remover {removendo.name}?"
    confirmLabel={apagarGravacoes ? `remover e apagar ${bytes(tam)}` : 'remover'}
    danger
    onconfirm={confirmarRemocao}
    oncancel={() => (removendo = null)}
  >
    A câmera sai do dwnvr e para de gravar agora.

    <!-- Desmarcado por padrão: apagar horas de vídeo não pode ser efeito
         colateral de um clique em "remover". O rótulo do botão acompanha a
         marcação para que a consequência esteja escrita no que se clica. -->
    <label class="check apagar">
      <input type="checkbox" bind:checked={apagarGravacoes} />
      apagar também as gravações ({bytes(tam)})
    </label>

    {#if !apagarGravacoes}
      Sem marcar, os arquivos ficam em disco e passam a aparecer aqui em
      <strong>Gravações de câmeras removidas</strong>, de onde dá para apagá-los depois.
    {/if}
  </ConfirmDialog>
{/if}

{#if confirmandoReinicio}
  {@const ativas = cameras.list.filter((c) => c.enabled).length}
  <ConfirmDialog
    title="Reiniciar o go2rtc?"
    confirmLabel="reiniciar"
    onconfirm={reiniciarGo2rtc}
    oncancel={() => (confirmandoReinicio = false)}
  >
    Ele volta a ler o <code>go2rtc.yaml</code>.
    {ativas === 1
      ? 'Enquanto isso, a câmera para de gravar por alguns segundos e volta sozinha.'
      : `Enquanto isso, as ${ativas} câmeras param de gravar por alguns segundos e voltam sozinhas.`}
    {#each vaoParar as id (id)}
      <p class="vai-parar">
        <code>{id}</code> saiu do arquivo: depois do reinício ela para de gravar.
      </p>
    {/each}
  </ConfirmDialog>
{/if}

{#if apagando}
  <ConfirmDialog
    title="Apagar as gravações de {apagando.nome}?"
    confirmLabel={apagando.bytes ? `apagar ${bytes(apagando.bytes)}` : 'apagar'}
    danger
    onconfirm={confirmarApagamento}
    oncancel={() => (apagando = null)}
  >
    {#if apagando.orfa}
      Tudo o que sobrou de <code>{apagando.id}</code> sai do disco, e não há como desfazer.
    {:else}
      Tudo o que essa câmera gravou sai do disco, e não há como desfazer. A câmera continua
      cadastrada e volta a gravar em seguida - a gravação fica interrompida por alguns segundos.
    {/if}
  </ConfirmDialog>
{/if}

<style>
  .page {
    display: grid;
    gap: 10px;
    padding: 10px;
    max-width: 900px;
    margin: 0 auto;
  }

  .list {
    display: grid;
    gap: 8px;
  }
  .cam {
    display: grid;
    gap: 8px;
  }
  .head {
    gap: 8px;
  }
  .acoes {
    gap: 8px;
    margin-left: auto;
  }
  .chips {
    gap: 6px;
  }
  /* O chip nasce nowrap, e um caminho longo não tem espaço para quebrar: sem
     estas duas linhas ele estoura a largura do card no celular. */
  .chip.storage {
    white-space: normal;
    overflow-wrap: anywhere;
  }
  /* Os dois falam da mesma cota e por isso compartilham a borda; a cor separa
     o que é fato do que é conta: "retido" já aconteceu e fica no texto normal,
     "cabem" é projeção e vai no tom do acento, como toda estimativa da tela. */
  .chip.retido,
  .chip.retain {
    border-color: #1f6feb66;
  }
  .chip.retain {
    color: var(--accent);
  }
  .chip.warn {
    color: var(--warn);
  }
  .chip.areas-chip {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding-left: 4px;
  }
  .err {
    color: var(--bad);
    margin: 0;
  }

  .warnbox {
    border-color: #6b5a1f;
    color: var(--warn);
  }
  .errbox {
    border-color: #5c2b2b;
    color: var(--bad);
  }

  h3 {
    margin: 0 0 4px;
    font-size: 15px;
  }

  /* O botão fica colado à direita do título, e não na lista: ele vale também
     quando a lista está vazia, que é justamente quando se desconfia dela. A
     margem negativa devolve a altura que o alvo de 44px acrescenta à linha. */
  .ajuda .titulo {
    justify-content: space-between;
    margin: -10px 0 0;
  }
  .ajuda h3 {
    margin: 0;
  }
  /* Sem borda em repouso: com ela, o botão pesava mais que o título ao lado. A
     borda de :hover e o foco do teclado continuam valendo. */
  .atualizar {
    display: inline-flex;
    align-items: center;
    padding: 0 12px;
    color: var(--dim);
    border-color: transparent;
  }
  .atualizar.girando :global(svg) {
    animation: girar 0.8s linear infinite;
  }
  @keyframes girar {
    to {
      transform: rotate(360deg);
    }
  }
  /* O aviso fica dentro do card, entre o título e a lista: é onde se olha
     depois de editar o arquivo, e é a lista que ele explica. */
  .aviso {
    display: grid;
    gap: 10px;
    margin: 0 0 12px;
    padding: 12px;
  }
  .aviso p {
    margin: 0;
  }
  .aviso ul {
    margin: 0;
    padding-left: 18px;
    color: var(--fg);
  }
  .aviso li {
    margin: 2px 0;
  }
  .aviso li .muted {
    font-size: 12px;
  }
  .aviso .acao {
    justify-content: flex-end;
  }
  button.warn {
    color: var(--warn);
    border-color: #6b5a1f;
    white-space: nowrap;
  }
  .reinicio {
    margin: 10px 0 0;
  }
  /* Com cara de link, porque é a saída de exceção; o alvo de toque vem do
     padding, já que 44px de altura mínima afastariam a linha do texto. */
  button.link {
    background: none;
    border: 0;
    min-height: 0;
    padding: 6px 2px;
    color: var(--accent);
    text-decoration: underline;
    font-size: inherit;
  }
  .vai-parar {
    margin: 8px 0 0;
    color: var(--warn);
  }
  .ajuda .vazio {
    margin: 0;
  }
  .ajuda .explica {
    margin: 12px 0 0;
  }
  .ajuda code {
    color: var(--fg);
  }

  .orfas {
    display: grid;
    gap: 8px;
  }
  .orfas p {
    margin: -4px 0 2px;
  }
  .orfa {
    gap: 10px;
    align-items: start;
  }
  .orfa .dados {
    gap: 6px;
    flex: 1;
    min-width: 0;
  }
  .orfa code {
    color: var(--fg);
  }
  .orfa button {
    flex: none;
  }

  /* O checkbox dentro do ConfirmDialog: sem a margem ele encosta no parágrafo
     acima e deixa de parecer uma escolha à parte. */
  label.check.apagar {
    margin: 10px 0 6px;
    font-size: 14px;
  }

  /* O Modal só entrega a moldura; o espaçamento entre os campos é do formulário. */
  .fields {
    display: grid;
    gap: 14px;
    align-content: start;
  }
  .paineis {
    display: grid;
  }
  .explica-areas,
  .aviso-areas {
    margin: 0;
  }
  .aviso-areas {
    color: var(--warn);
  }
  .areas {
    display: grid;
    gap: 8px;
  }
  .area-item {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .miniatura {
    position: relative;
    flex: none;
    width: 96px;
    border-radius: 4px;
    overflow: hidden;
    background: #000;
  }
  .miniatura img,
  .miniatura svg {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    display: block;
  }
  .miniatura polygon {
    fill: rgba(47, 129, 247, 0.22);
    stroke: var(--accent);
    stroke-width: 1.5;
    vector-effect: non-scaling-stroke;
  }
  .nome-area {
    display: grid;
    flex: 1;
    min-width: 0;
  }
  .nome-area b {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  button.nova-area {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    justify-self: start;
  }
  button.passo {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .paineis > * {
    grid-area: 1 / 1;
  }
  .paineis > [inert] {
    visibility: hidden;
  }

  label {
    display: grid;
    gap: 5px;
    font-size: 13px;
    color: var(--dim);
  }
  label input:not([type='checkbox']),
  label select {
    width: 100%;
    color: var(--fg);
    font-size: 15px;
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
    accent-color: var(--accent);
  }
  small {
    font-size: 12px;
  }
</style>
