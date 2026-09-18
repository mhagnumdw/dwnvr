// Textos de ajuda compartilhados entre telas, pela mesma razão que os
// formatadores moram juntos em format.js: a coluna do Diagnóstico e o chip da
// tela de Câmeras mostram a MESMA grandeza, e se cada tela escrevesse a sua
// explicação as duas divergiriam na primeira alteração de uma delas.

// O par retido/cabem é o que torna a cota compreensível: um é o passado que
// existe, o outro é o que ela ainda comporta. Sozinho, cada um engana.
export const AJUDA_RETIDO = 'Tempo total de gravação em disco (incluídos períodos de inatividade).';

export const AJUDA_CABEM =
  'Quanto tempo de gravação cabe na cota, pelo consumo médio do que a câmera já gravou. É estimativa: encolhe se ela passar a gastar mais por dia. Com a cota cheia, encosta no retido.';

// A escada de sensibilidade da detecção de movimento. Ela vive aqui, e não na
// tela, porque o rótulo e o custo aparecem em dois lugares - o chip da lista e
// o formulário.
//
// `porHora` é o custo POR CÂMERA, e repete à mão o OnsetsPorHora de
// internal/detect/parametros.go: com dez câmeras no nível 4 são 500 olhadas
// por hora, não 50.
//
// O índice zero não existe: os níveis vão de 1 a 5, e a posição vazia é o que
// permite indexar direto pelo número escolhido.
export const NIVEIS = [
  null,
  { rotulo: 'muito baixa', porHora: 6 },
  { rotulo: 'baixa', porHora: 12 },
  { rotulo: 'média', porHora: 25 },
  { rotulo: 'alta', porHora: 50 },
  { rotulo: 'muito alta', porHora: 100 },
];

export const AJUDA_SENSIBILIDADE =
  'Quantas vezes por hora, nesta câmera, o dwnvr para para olhar a imagem. Subir o nível pega mais movimento e custa mais CPU.';
