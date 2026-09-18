#!/usr/bin/env python3
"""dwnvr-detect: o detector de objetos do dwnvr, num container à parte.

O dwnvr manda o pedaço de vídeo que o recorder cortou - o init da conexão e os
fragmentos do frame I até o quadro a olhar - e recebe o que havia no ÚLTIMO
quadro dele. Nada mais: sem estado, sem fila, sem decidir o que vira marca.
Isso tudo é do dwnvr, que é Go puro e não pode carregar decodificador de vídeo
nem runtime de modelo.

  POST /detect?piso=0.20    corpo: o pedaço (.mp4)   -> {"achados": [...], ...}
  GET  /health                                       -> o modelo carregado

Faz UMA detecção por vez, de propósito: numa placa de 4 núcleos que também
está gravando, duas em paralelo seriam o dobro de RAM de pico sem uma resposta
a mais por segundo. Quem enfileira é o dwnvr. O `/health` não entra
nessa vez: responde na hora mesmo com uma detecção rodando, senão o
healthcheck do Docker, que espera 4 s, desiste no meio de uma olhada de 3,6 s.

O preparo (`prepara`) é o mesmo que calibrou o int8: o `modelo/exporta.py` o
importa daqui. Se os dois divergissem, o modelo rodaria sobre uma faixa
numérica diferente da que foi calibrada.

Configuração por variável de ambiente:

  DETECT_MODELO   o .onnx                       (/app/modelo.onnx)
  DETECT_THREADS  threads do modelo e do vídeo  (1)
  DETECT_PORTA    porta HTTP                    (8480)

A porta não é a 8555 de propósito: é a do WebRTC do go2rtc, que roda ao lado.
"""
from __future__ import annotations

import http.server
import io
import json
import os
import socket
import sys
import threading
import time
import urllib.parse

import av
import numpy as np
import onnxruntime as ort

MODELO = os.environ.get("DETECT_MODELO", "/app/modelo.onnx")
THREADS = int(os.environ.get("DETECT_THREADS", "1"))
PORTA = int(os.environ.get("DETECT_PORTA", "8480"))

#: O piso de quem não disser outro. O dwnvr diz: é o `PisoDoDetector` dele.
PISO_PADRAO = 0.20

#: Maior pedaço aceito. O dwnvr não guarda GOP maior que 4 MB por câmera
#: (`TetoDoGOPBytes`); isto é só para um cliente errado não esgotar a RAM.
MAIOR_PEDACO = 16 << 20

#: Normalização do ImageNet, a do treino do RF-DETR.
IMAGENET_MEDIA = (0.485, 0.456, 0.406)
IMAGENET_DESVIO = (0.229, 0.224, 0.225)

#: Índice de saída do RF-DETR -> classe. É COCO de 91, com buracos (person=1),
#: e não o de 80 do YOLO: ler um com o mapa do outro troca pessoa por bicicleta
#: calado. Copiado de `rfdetr.assets.coco_classes`, que não vai para a imagem.
CLASSES = {
    1: "person", 2: "bicycle", 3: "car", 4: "motorcycle", 5: "airplane", 6: "bus",
    7: "train", 8: "truck", 9: "boat", 10: "traffic light", 11: "fire hydrant",
    13: "stop sign", 14: "parking meter", 15: "bench", 16: "bird", 17: "cat",
    18: "dog", 19: "horse", 20: "sheep", 21: "cow", 22: "elephant", 23: "bear",
    24: "zebra", 25: "giraffe", 27: "backpack", 28: "umbrella", 31: "handbag",
    32: "tie", 33: "suitcase", 34: "frisbee", 35: "skis", 36: "snowboard",
    37: "sports ball", 38: "kite", 39: "baseball bat", 40: "baseball glove",
    41: "skateboard", 42: "surfboard", 43: "tennis racket", 44: "bottle",
    46: "wine glass", 47: "cup", 48: "fork", 49: "knife", 50: "spoon", 51: "bowl",
    52: "banana", 53: "apple", 54: "sandwich", 55: "orange", 56: "broccoli",
    57: "carrot", 58: "hot dog", 59: "pizza", 60: "donut", 61: "cake",
    62: "chair", 63: "couch", 64: "potted plant", 65: "bed", 67: "dining table",
    70: "toilet", 72: "tv", 73: "laptop", 74: "mouse", 75: "remote",
    76: "keyboard", 77: "cell phone", 78: "microwave", 79: "oven", 80: "toaster",
    81: "sink", 82: "refrigerator", 84: "book", 85: "clock", 86: "vase",
    87: "scissors", 88: "teddy bear", 89: "hair drier", 90: "toothbrush",
}


class PedacoRuim(Exception):
    """O pedaço não decodifica até o fim. Olhar o último quadro que sobrou
    seria olhar outro instante, e isso é pior que não olhar."""


# ------------------------------------------------------------------ o vídeo

def ultimo_quadro(fmp4: bytes) -> tuple[int, np.ndarray]:
    """Decodifica o pedaço inteiro e devolve (quadros decodificados, o último
    em RGB). O pedaço começa num frame I, então decodifica sozinho."""
    n, ultimo = 0, None
    try:
        with av.open(io.BytesIO(fmp4), format="mp4") as c:
            st = c.streams.video[0]
            st.codec_context.thread_count = THREADS
            for fr in c.decode(st):
                n, ultimo = n + 1, fr
    except (av.error.FFmpegError, IndexError) as ex:
        raise PedacoRuim(str(ex)[:200]) from ex
    if ultimo is None:
        raise PedacoRuim("nenhum quadro no pedaço")
    return n, ultimo.to_ndarray(format="rgb24")


# ------------------------------------------------------------------ o preparo

def bilinear(img: np.ndarray, ah: int, aw: int) -> np.ndarray:
    """Bilinear SEM antialias, com centros de meio pixel - a convenção do
    `F.resize(antialias=False)` que o RF-DETR usa. O `PIL.BILINEAR` não é isso:
    ao reduzir ele faz média de área, e a fidelidade cai."""
    h, w = img.shape[:2]
    y = (np.arange(ah, dtype=np.float32) + 0.5) * (h / ah) - 0.5
    x = (np.arange(aw, dtype=np.float32) + 0.5) * (w / aw) - 0.5
    y0 = np.floor(y).astype(np.int32)
    x0 = np.floor(x).astype(np.int32)
    py = (y - y0)[:, None, None]
    px = (x - x0)[None, :, None]
    ya, yb = np.clip(y0, 0, h - 1), np.clip(y0 + 1, 0, h - 1)
    xa, xb = np.clip(x0, 0, w - 1), np.clip(x0 + 1, 0, w - 1)
    f = img.astype(np.float32)
    cima = f[ya][:, xa] + (f[ya][:, xb] - f[ya][:, xa]) * px
    baixo = f[yb][:, xa] + (f[yb][:, xb] - f[yb][:, xa]) * px
    return (cima + (baixo - cima) * py).astype(np.float32, copy=False)


def prepara(img: np.ndarray, ah: int, aw: int) -> np.ndarray:
    """Quadro da câmera -> tensor do RF-DETR: estica até a entrada, sem barra
    (é o que o predict() dele faz), e normaliza pelo ImageNet."""
    x = img.astype(np.float32) if img.shape[:2] == (ah, aw) else bilinear(img, ah, aw)
    x = x / 255.0
    x = (x - np.array(IMAGENET_MEDIA, dtype=np.float32)) / np.array(IMAGENET_DESVIO, dtype=np.float32)
    return np.ascontiguousarray(x.transpose(2, 0, 1)[None], dtype=np.float32)


# ------------------------------------------------------------------ o modelo

class Modelo:
    def __init__(self, caminho: str):
        ort.disable_telemetry_events()
        o = ort.SessionOptions()
        # O onnxruntime não obedece OMP_NUM_THREADS nem o limite de CPU do
        # container: o número de threads dele é este, e só este.
        o.intra_op_num_threads = THREADS
        o.inter_op_num_threads = 1
        self.sessao = ort.InferenceSession(caminho, o, providers=["CPUExecutionProvider"])
        entrada = self.sessao.get_inputs()[0]
        self.entrada = entrada.name
        _, _, self.ah, self.aw = entrada.shape
        self.nome = os.path.basename(caminho)

    def olha(self, img: np.ndarray, piso: float) -> tuple[list, dict]:
        t0 = time.perf_counter()
        x = prepara(img, self.ah, self.aw)
        t1 = time.perf_counter()
        dets, logits = (s[0] for s in self.sessao.run(None, {self.entrada: x}))
        t2 = time.perf_counter()

        # (300, 4) em cxcywh normalizado, e (300, 91) de logits. Como o
        # RF-DETR estica o quadro, a caixa normalizada JÁ é fração do quadro
        # da câmera - qualquer que seja a resolução dela.
        conf = 1 / (1 + np.exp(-logits))
        escores, classes = conf.max(1), conf.argmax(1)
        achados = []
        for i in np.flatnonzero(escores >= piso):
            classe = CLASSES.get(int(classes[i]))
            if classe is None:
                continue  # índice de enchimento do COCO de 91
            cx, cy, w, h = dets[i]
            achados.append({
                "classe": classe,
                "score": float(escores[i]),
                "caixa": [float(cx - w / 2), float(cy - h / 2),
                          float(cx + w / 2), float(cy + h / 2)],
            })
        return achados, {"preparo": round((t1 - t0) * 1000, 1),
                         "modelo": round((t2 - t1) * 1000, 1)}


# ------------------------------------------------------------------ o HTTP

class Atendente(http.server.BaseHTTPRequestHandler):
    modelo: Modelo  # posto no main
    vez = threading.Lock()  # uma detecção de cada vez

    def _responde(self, codigo: int, corpo: dict):
        b = json.dumps(corpo).encode()
        self.send_response(codigo)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)

    def do_GET(self):
        if self.path != "/health":
            return self._responde(404, {"erro": "não existe"})
        m = self.modelo
        self._responde(200, {"modelo": m.nome, "entrada": f"{m.aw}x{m.ah}", "threads": THREADS})

    def do_POST(self):
        url = urllib.parse.urlsplit(self.path)
        if url.path != "/detect":
            return self._responde(404, {"erro": "não existe"})
        try:
            piso = float(urllib.parse.parse_qs(url.query).get("piso", [PISO_PADRAO])[0])
        except ValueError:
            return self._responde(400, {"erro": "piso ilegível"})
        tamanho = int(self.headers.get("Content-Length") or 0)
        if tamanho <= 0 or tamanho > MAIOR_PEDACO:
            return self._responde(413 if tamanho > 0 else 400,
                                  {"erro": f"pedaço de {tamanho} bytes"})
        corpo = self.rfile.read(tamanho)

        with self.vez:
            t0 = time.perf_counter()
            try:
                n, img = ultimo_quadro(corpo)
            except PedacoRuim as ex:
                return self._responde(422, {"erro": f"pedaço não decodifica: {ex}"})
            t1 = time.perf_counter()
            achados, tempos = self.modelo.olha(img, piso)
            tempos["decodifica"] = round((t1 - t0) * 1000, 1)
        self._responde(200, {
            "achados": achados,
            "quadrosDecodificados": n,
            "largura": int(img.shape[1]),
            "altura": int(img.shape[0]),
            "tempoMs": tempos,
        })

    def log_message(self, formato, *args):
        pass  # centenas de pedidos por hora; quem conta é a telemetria do dwnvr


class Servidor(http.server.ThreadingHTTPServer):
    daemon_threads = True

    def handle_error(self, request, client_address):
        # Cliente que desistiu antes da resposta - o dwnvr no prazo dele, ou o
        # healthcheck - não é erro do servidor, e não merece um traceback.
        if isinstance(sys.exc_info()[1], (BrokenPipeError, ConnectionResetError, socket.timeout)):
            return
        super().handle_error(request, client_address)


def main():
    Atendente.modelo = Modelo(MODELO)
    m = Atendente.modelo
    print(f"dwnvr-detect: {m.nome}, entrada {m.aw}x{m.ah}, {THREADS} thread(s), "
          f"porta {PORTA}", file=sys.stderr, flush=True)
    Servidor(("", PORTA), Atendente).serve_forever()


if __name__ == "__main__":
    main()
