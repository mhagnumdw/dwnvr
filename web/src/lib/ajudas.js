// Textos de ajuda compartilhados entre telas, pela mesma razão que os
// formatadores moram juntos em format.js: a coluna do Diagnóstico e o chip da
// tela de Câmeras mostram a MESMA grandeza, e se cada tela escrevesse a sua
// explicação as duas divergiriam na primeira alteração de uma delas.

// O par retido/cabem é o que torna a cota compreensível: um é o passado que
// existe, o outro é o que ela ainda comporta. Sozinho, cada um engana.
export const AJUDA_RETIDO = 'Tempo total de gravação em disco (incluídos períodos de inatividade).';

export const AJUDA_CABEM =
  'Quantos dias de gravação cabem na cota da câmera na taxa medida agora. É estimativa: se a taxa subir, o passado encolhe.';
