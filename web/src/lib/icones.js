// As famílias de objeto e os ícones que as desenham na timeline.
//
// Os desenhos são path de canvas, e não
// emoji nem fonte de ícone: o emoji muda de desenho a cada sistema e não aceita
// cor, e uma fonte seria um arquivo a mais num projeto que entrega um binário
// só. A 14px o que decide a leitura é a silhueta, e aqui a silhueta é nossa.

// As três famílias, na ordem de prioridade: `pessoa` é o que a detecção existe
// para achar, `animal` é o que menos importa. A prioridade decide quem fica por
// cima quando duas marcas se sobrepõem e quem ganha o lugar de ícone quando
// não cabem todos. O nome da família é o que o servidor manda em `familia`.
export const FAMILIAS = {
  pessoa: { nome: 'pessoa', cor: '#3fb950', prioridade: 0 },
  veiculo: { nome: 'veículo', cor: '#a371f7', prioridade: 1 },
  animal: { nome: 'animal', cor: '#db61a2', prioridade: 2 },
};

// Família que o servidor mandar e esta tela não conhecer ainda desenha em
// cinza e fica atrás das outras: aparecer é melhor que sumir.
export const FAMILIA_DESCONHECIDA = { nome: '?', cor: '#8b949e', prioridade: 9 };

export const familia = (nome) => FAMILIAS[nome] ?? FAMILIA_DESCONHECIDA;

// As classes do modelo (nomes do COCO, como o detector devolve) em português,
// para a dica do ponteiro. Espelha o `FamiliasPadrao` de
// internal/detect/parametros.go; classe fora dele aparece com o nome cru.
export const NOME_DA_CLASSE = {
  person: 'pessoa',
  bicycle: 'bicicleta',
  car: 'carro',
  motorcycle: 'moto',
  bus: 'ônibus',
  truck: 'caminhão',
  train: 'trem',
  cat: 'gato',
  dog: 'cachorro',
  horse: 'cavalo',
  sheep: 'ovelha',
  cow: 'vaca',
};

// Todo ícone é desenhado num quadrado 0..1 e escalado por `desenhaIcone`, para
// o mesmo desenho servir a 14px e a 20px.
const ICONES = {
  pessoa(g) {
    g.beginPath(); g.arc(0.5, 0.18, 0.16, 0, 7); g.fill();       // cabeça
    g.beginPath();
    g.moveTo(0.5, 0.36); g.lineTo(0.5, 0.68);                    // tronco
    g.moveTo(0.22, 0.48); g.lineTo(0.78, 0.48);                  // braços
    g.moveTo(0.5, 0.68); g.lineTo(0.28, 1);                      // pernas
    g.moveTo(0.5, 0.68); g.lineTo(0.72, 1);
    g.stroke();
  },
  // As rodas SAEM por baixo da linha do corpo e a cabine é um trapézio à
  // parte: com rodas encostadas no corpo, a 14px o carro lia como bicho.
  carro(g) {
    g.beginPath();
    g.moveTo(0.3, 0.34); g.lineTo(0.38, 0.16); g.lineTo(0.66, 0.16); g.lineTo(0.72, 0.34);
    g.closePath(); g.fill();
    g.beginPath(); g.rect(0.05, 0.34, 0.9, 0.3); g.fill();
    g.beginPath(); g.arc(0.27, 0.72, 0.15, 0, 7); g.fill();
    g.beginPath(); g.arc(0.73, 0.72, 0.15, 0, 7); g.fill();
  },
  // Moto e bicicleta se separam pelo PESO, porque nenhum detalhe sobrevive a
  // 14px: a moto é maciça, a bicicleta é vazada.
  moto(g) {
    g.beginPath(); g.arc(0.21, 0.7, 0.21, 0, 7); g.fill();
    g.beginPath(); g.arc(0.79, 0.7, 0.21, 0, 7); g.fill();
    g.beginPath();
    g.moveTo(0.18, 0.62); g.lineTo(0.42, 0.34); g.lineTo(0.74, 0.34); g.lineTo(0.84, 0.62);
    g.closePath(); g.fill();
    g.beginPath(); g.rect(0.3, 0.14, 0.22, 0.09); g.fill();      // guidão
  },
  bicicleta(g) {
    g.beginPath(); g.arc(0.19, 0.68, 0.19, 0, 7); g.stroke();
    g.beginPath(); g.arc(0.81, 0.68, 0.19, 0, 7); g.stroke();
    g.beginPath();
    g.moveTo(0.19, 0.68); g.lineTo(0.46, 0.68); g.lineTo(0.6, 0.3);
    g.lineTo(0.81, 0.68); g.moveTo(0.46, 0.68); g.lineTo(0.6, 0.3);
    g.moveTo(0.5, 0.26); g.lineTo(0.7, 0.26);
    g.stroke();
  },
  onibus(g) {
    g.beginPath(); g.rect(0.08, 0.2, 0.84, 0.56); g.fill();
    vazado(g, () => {                                            // janelas
      g.rect(0.16, 0.28, 0.3, 0.2);
      g.rect(0.54, 0.28, 0.3, 0.2);
    });
  },
  caminhao(g) {
    g.beginPath(); g.rect(0.04, 0.28, 0.48, 0.44); g.fill();     // baú
    g.beginPath();
    g.moveTo(0.56, 0.44); g.lineTo(0.78, 0.44); g.lineTo(0.94, 0.6);
    g.lineTo(0.94, 0.72); g.lineTo(0.56, 0.72); g.closePath(); g.fill();
    g.beginPath(); g.arc(0.26, 0.8, 0.12, 0, 7); g.fill();
    g.beginPath(); g.arc(0.78, 0.8, 0.12, 0, 7); g.fill();
  },
  trem(g) {
    g.beginPath(); g.rect(0.14, 0.16, 0.72, 0.52); g.fill();
    vazado(g, () => g.rect(0.24, 0.26, 0.52, 0.2));              // janela
    g.beginPath(); g.arc(0.3, 0.82, 0.11, 0, 7); g.fill();
    g.beginPath(); g.arc(0.7, 0.82, 0.11, 0, 7); g.fill();
  },
  // Cachorro e gato se separam pela silhueta inteira, porque orelha some a
  // 14px: o gato tem o rabo erguido, o cachorro tem rabo curto e focinho longo.
  cachorro(g) {
    g.beginPath(); g.rect(0.16, 0.52, 0.48, 0.22); g.fill();     // corpo
    g.beginPath();
    g.moveTo(0.24, 0.74); g.lineTo(0.24, 0.94);
    g.moveTo(0.56, 0.74); g.lineTo(0.56, 0.94);
    g.stroke();
    g.beginPath(); g.arc(0.72, 0.46, 0.15, 0, 7); g.fill();      // cabeça
    g.beginPath(); g.rect(0.8, 0.46, 0.18, 0.1); g.fill();       // focinho
    g.beginPath(); g.moveTo(0.16, 0.56); g.lineTo(0.02, 0.48); g.stroke();
  },
  gato(g) {
    g.beginPath(); g.rect(0.2, 0.56, 0.42, 0.2); g.fill();
    g.beginPath();
    g.moveTo(0.28, 0.76); g.lineTo(0.28, 0.94);
    g.moveTo(0.56, 0.76); g.lineTo(0.56, 0.94);
    g.stroke();
    g.beginPath(); g.arc(0.72, 0.5, 0.14, 0, 7); g.fill();
    g.beginPath();                                               // orelhas
    g.moveTo(0.61, 0.4); g.lineTo(0.6, 0.24); g.lineTo(0.72, 0.38); g.closePath(); g.fill();
    g.beginPath();
    g.moveTo(0.83, 0.4); g.lineTo(0.86, 0.24); g.lineTo(0.74, 0.38); g.closePath(); g.fill();
    g.beginPath();                                               // rabo erguido
    g.moveTo(0.2, 0.66); g.quadraticCurveTo(0.02, 0.58, 0.08, 0.16); g.stroke();
  },
  // O animal da família é um quadrúpede sem orelha de propósito: quando ainda
  // não dá para distinguir, ele não pode sugerir cachorro nem gato.
  animal(g) {
    g.beginPath();
    g.moveTo(0.12, 0.84); g.lineTo(0.12, 0.54); g.lineTo(0.62, 0.54);
    g.lineTo(0.62, 0.84); g.moveTo(0.37, 0.54); g.lineTo(0.37, 0.84);
    g.stroke();
    g.beginPath(); g.arc(0.76, 0.42, 0.17, 0, 7); g.fill();
    g.beginPath(); g.moveTo(0.12, 0.54); g.lineTo(0.02, 0.34); g.stroke();
  },
};
// O veículo da família é o carro, a classe mais comum dela.
ICONES.veiculo = ICONES.carro;

// Recorta o desenho, em vez de pintar por cima com a cor do fundo: o ícone
// fica sobre a página, e o furo mostra a página, seja qual for a cor dela.
function vazado(g, caminho) {
  g.save();
  g.globalCompositeOperation = 'destination-out';
  g.beginPath();
  caminho();
  g.fill();
  g.restore();
}

// A classe do modelo -> o ícone próprio dela. Classe sem ícone (cavalo,
// ovelha, vaca) desenha o da família.
const ICONE_DA_CLASSE = {
  person: 'pessoa',
  bicycle: 'bicicleta',
  car: 'carro',
  motorcycle: 'moto',
  bus: 'onibus',
  truck: 'caminhao',
  train: 'trem',
  cat: 'gato',
  dog: 'cachorro',
};

// O nome do ícone de uma marca no nível de detalhe pedido.
export function iconeDa(marca, porClasse) {
  const nome = porClasse ? ICONE_DA_CLASSE[marca.classe] || marca.familia : marca.familia;
  return ICONES[nome] ? nome : null;
}

// Desenha um ícone com o canto superior esquerdo em (x, y) e `lado` px.
export function desenhaIcone(g, nome, x, y, lado, cor) {
  const f = ICONES[nome];
  if (!f) return;
  g.save();
  g.translate(x, y);
  g.scale(lado, lado);
  g.fillStyle = cor;
  g.strokeStyle = cor;
  g.lineWidth = 2 / lado; // 2px reais, seja qual for a escala
  g.lineJoin = 'round';
  g.lineCap = 'round';
  f(g);
  g.restore();
}

// O mesmo ícone como imagem, para quem desenha em HTML e não em canvas: a
// grade de Detecções põe o ícone em cada miniatura, e um <canvas> por ícone
// seria um contexto 2D por miniatura. Cada combinação é desenhada uma vez só,
// na densidade da tela, e reaproveitada por todas.
const imagens = new Map();

export function iconeURL(nome, lado, cor) {
  const dpr = window.devicePixelRatio || 1;
  const chave = `${nome}|${lado}|${cor}|${dpr}`;
  let url = imagens.get(chave);
  if (!url) {
    const c = document.createElement('canvas');
    c.width = c.height = Math.ceil(lado * dpr);
    const g = c.getContext('2d');
    g.scale(dpr, dpr);
    desenhaIcone(g, nome, 0, 0, lado, cor);
    url = c.toDataURL();
    imagens.set(chave, url);
  }
  return url;
}
