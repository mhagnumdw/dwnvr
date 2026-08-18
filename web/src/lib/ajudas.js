// Textos de ajuda compartilhados entre telas, pela mesma razão que os
// formatadores moram juntos em format.js: a coluna do Diagnóstico e o chip da
// tela de Câmeras mostram a MESMA grandeza, e se cada tela escrevesse a sua
// explicação as duas divergiriam na primeira alteração de uma delas.

// O par retido/cabem é o que torna a cota compreensível: um é o passado que
// existe, o outro é o que ela ainda comporta. Sozinho, cada um engana.
export const AJUDA_RETIDO = 'Tempo total de gravação em disco (incluídos períodos de inatividade).';

export const AJUDA_CABEM =
  'Quanto tempo de gravação cabe na cota, pelo consumo médio do que a câmera já gravou. É estimativa: encolhe se ela passar a gastar mais por dia. Com a cota cheia, encosta no retido.';
