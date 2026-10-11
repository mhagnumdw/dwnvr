// Tipos do video-rtc.js, escritos à mão: o .js é do go2rtc, copiado como veio,
// e precisa continuar se atualizando com um `curl`. Aqui só o que o dwnvr usa,
// com os tipos dos comentários JSDoc do próprio arquivo.

export declare class VideoRTC extends HTMLElement {
  /** Carência, em ms, entre sair da tela e fechar a conexão. */
  DISCONNECT_TIMEOUT: number;
  /** Intervalo mínimo, em ms, entre uma tentativa de conexão e a seguinte. */
  RECONNECT_TIMEOUT: number;
  /** Modos aceitos, em ordem de preferência: `webrtc`, `mse`, `hls`, `mjpeg`. */
  mode: string;
  /** O que pedir ao go2rtc: `video`, `audio`, `microphone`. */
  media: string;
  /** Segue tocando fora da tela. */
  background: boolean;
  /** Fração visível do player para ele tocar; 0 desliga a regra. */
  visibilityThreshold: number;
  /** Para de tocar com a aba oculta. */
  visibilityCheck: boolean;
  /** Nasce no primeiro `connectedCallback`: nulo antes disso. */
  video: HTMLVideoElement | null;
  /** O endereço do websocket do go2rtc. Aceita caminho começado por `/`. */
  set src(value: string | URL);

  connectedCallback(): void;
  disconnectedCallback(): void;
  /** Abre a conexão; `true` se abriu, `false` se não havia o que abrir. */
  onconnect(): boolean;
  /** Fecha o websocket e o WebRTC, e solta o vídeo. */
  ondisconnect(): void;
  /** O websocket fechou; `true` se agendou outra tentativa. */
  onclose(): boolean;
}
