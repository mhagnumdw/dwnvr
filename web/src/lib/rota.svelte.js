// Roteamento por hash, com o estado da tela junto dele.
//
// O hash é `#<rota>?<query>`: a rota escolhe a tela, a query descreve o que se
// está vendo dentro dela. É isso que faz copiar a barra de endereços e colar em
// outra aba cair na mesma cena - as mesmas câmeras, o mesmo instante, o mesmo
// zoom - em vez da tela genérica.
//
// Continua sendo hash, e não caminho de verdade, por um motivo prático: o
// fragmento nunca sai do navegador, então nenhuma rota nova precisa existir do
// lado do servidor. O `internal/api/web.go` só serve arquivo, e um `/rec` real
// exigiria devolver o index.html para todo caminho que não existe.

export const ROTA_PADRAO = 'live';

// Piso entre duas escritas na barra de endereços. O instante do player muda
// várias vezes por segundo, e o Safari recusa mais de 100 replaceState em 30s;
// um segundo deixa ~30 dentro da janela que ele conta, com folga de sobra.
const PISO_MS = 1000;

function partes(hash) {
  const cru = hash.replace(/^#/, '');
  const i = cru.indexOf('?');
  return i < 0
    ? { id: cru || ROTA_PADRAO, query: '' }
    : { id: cru.slice(0, i) || ROTA_PADRAO, query: cru.slice(i + 1) };
}

// A leitura é do URLSearchParams, que é o que a plataforma já traz: ele
// decodifica, tolera sequência inválida de uma URL escrita à mão e resolve
// parâmetro repetido com getAll. A ESCRITA é própria só por causa de dois
// caracteres: o toString() dele percent-encoda `:` e `,`, e `t=14%3A32%3A07`
// jogaria fora a legibilidade que é o motivo de escrever o horário assim. Os
// dois são permitidos em query pela RFC 3986, e a leitura aceita as duas formas
// - então link antigo ou copiado de outro lugar continua valendo.
const enc = (s) => encodeURIComponent(s).replaceAll('%3A', ':').replaceAll('%2C', ',');

function junta(sp) {
  const out = [];
  for (const [k, v] of sp) out.push(`${enc(k)}=${enc(v)}`);
  return out.join('&');
}

export const rota = $state({
  id: partes(location.hash).id,
  // O hash inteiro, como estava na última navegação DE VERDADE. É a chave que
  // remonta a tela: as escritas daqui usam replaceState, que não dispara
  // hashchange, então elas não tocam neste valor e não remontam nada.
  hash: location.hash,
});

// paramsAtuais lê a query de agora. As telas chamam uma vez, na inicialização:
// a URL é a fonte da PRIMEIRA leitura, e daí em diante quem manda é o estado da
// tela, que escreve de volta.
export function paramsAtuais() {
  return new URLSearchParams(partes(location.hash).query);
}

let pendente = null; // o que ainda não chegou à barra de endereços
let timer = null;
let ultimaEscrita = 0;

// escrever funde um punhado de chaves no que já está na URL.
//
// - null ou undefined apaga a chave. É assim que o que está no padrão fica de
//   fora do link.
// - array vira parâmetro repetido (`cams=a&cams=b`), que é como um formulário
//   HTML serializa escolha múltipla. Sem separador não há o que confundir: o id
//   da câmera vem do nome do stream no go2rtc e pode conter vírgula.
// - array vazio escreve a chave sem valor, porque "nenhuma câmera marcada" é
//   uma escolha, diferente de não ter opinião nenhuma sobre a seleção.
export function escrever(patch) {
  const sp = pendente ?? paramsAtuais();
  for (const [k, v] of Object.entries(patch)) {
    sp.delete(k);
    if (v === null || v === undefined) continue;
    if (!Array.isArray(v)) sp.set(k, String(v));
    else if (!v.length) sp.set(k, '');
    else for (const item of v) sp.append(k, item);
  }
  pendente = sp;

  if (timer) return;
  const espera = PISO_MS - (Date.now() - ultimaEscrita);
  if (espera <= 0) aplica();
  else timer = setTimeout(aplica, espera);
}

function aplica() {
  timer = null;
  if (!pendente) return;
  const query = junta(pendente);
  pendente = null;

  const alvo = `#${rota.id}${query ? `?${query}` : ''}`;
  // Nada a fazer se a barra de endereços já diz isso, e durante a reprodução
  // esse é o caso comum: `t` é escrito em segundos, então a maior parte dos
  // quadros não muda letra nenhuma. Sem escrita, o piso também não corre - o
  // segundo seguinte aparece na hora, sem esperar o intervalo.
  if (location.hash === alvo) return;

  ultimaEscrita = Date.now();
  try {
    history.replaceState(history.state, '', alvo);
  } catch {
    // O Safari lança ao passar do limite de replaceState. Perder uma escrita da
    // barra de endereços não pode derrubar a tela: a próxima conserta.
  }
}

addEventListener('hashchange', () => {
  // A tela que está saindo pode ter deixado uma escrita agendada. Ela descreve a
  // rota antiga, e cair na rota nova seria pura sujeira - `#live?cam=x&day=…`.
  clearTimeout(timer);
  timer = null;
  pendente = null;

  rota.id = partes(location.hash).id;
  rota.hash = location.hash;
});
