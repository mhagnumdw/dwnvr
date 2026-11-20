#!/usr/bin/env python3
"""dwnvr-detect: dwnvr's object detector, in a container of its own.

dwnvr sends the video clip the recorder cut - the connection's init segment and
the fragments from the I frame up to the frame to inspect - and gets back what
was in the LAST frame of it. Nothing else: no state, no queue, no deciding what
becomes a mark. All of that is dwnvr's job, and dwnvr is pure Go, so it can't
carry a video decoder or a model runtime.

  POST /detect?minScore=0.20&frameWidth=480&quality=75
                        body: the clip (.mp4)    -> {"detections": [...], ...}
  GET  /health                                   -> the loaded model

With `frameWidth`, and when there is a detection, the response also
carries the inspected frame as a JPEG, scaled down to that width and in
base64. It is the thumbnail of the Detections screen: the frame comes from
here because here it is already decoded - dwnvr has no way to decode video -,
and the box, which travels as a fraction, lands in the right place over it at
any size.

Runs ONE inference at a time, on purpose: on a 4-core board that is also
recording, two in parallel would be double the peak RAM without one more answer
per second. dwnvr does the queueing. `/health` doesn't take that turn: it
answers right away even with an inference running, otherwise Docker's
healthcheck, which waits 4 s, would give up in the middle of a 3.6 s inference.

An inference that goes past `INFERENCE_DEADLINE_S` is treated as stuck: the
process exits with an error, and the compose's `restart: unless-stopped` brings
it back up. Without that, a stuck inference would leave the process alive,
`/health` answering and no object marks until someone restarted the container
by hand.

The preparation (`prepare`) is the same one that calibrated the int8:
`model/export.py` imports it from here. If the two diverged, the model would
run on a different numeric range from the one it was calibrated for.

Configuration by environment variable:

  DETECT_MODEL    the .onnx                     (/app/model.onnx)
  DETECT_THREADS  threads of the model and video (auto)
  DETECT_PORT     HTTP port                     (8480)

`auto` is half the cores the container sees, up to 4, and never above its CPU
limit: see `auto_threads`. The port isn't 8555 on purpose: that one is
go2rtc's WebRTC, which runs alongside.
"""

from __future__ import annotations

import base64
import fractions
import http.server
import io
import json
import os
import signal
import socket
import sys
import threading
import time
import urllib.parse

import av
import numpy as np
import onnxruntime as ort

MODEL_PATH = os.environ.get("DETECT_MODEL", "/app/model.onnx")
PORT = int(os.environ.get("DETECT_PORT", "8480"))

#: Cap of `DETECT_THREADS=auto`. Measured on the model alone, on a laptop i7:
#: from 2 to 4 threads the inference gets 1.7x faster for 15 to 19% more CPU,
#: the same trade as 1 to 2; from 4 to 6, only 1.2 to 1.3x. And the work doesn't
#: grow with the machine: it is the inferences the cameras ask for.
AUTO_THREADS_CAP = 4

#: The min score for whoever doesn't give another. dwnvr does: it is its
#: `MinScore`.
DEFAULT_MIN_SCORE = 0.20

#: Largest clip accepted. dwnvr doesn't keep a GOP larger than 4 MB per camera
#: (`MaxGOPBytes`); this is only so a wrong client can't exhaust the RAM.
MAX_CLIP_BYTES = 16 << 20

#: Largest width accepted for the frame sent back. Above that the JPEG would
#: cost more bandwidth and time than any screen uses; dwnvr asks for 480.
MAX_FRAME_WIDTH = 1920

#: Past this the inference is stuck, not slow: one takes ~6.5 s on one core of
#: an Orange Pi Zero 3. It stays below the 60 s dwnvr waits for
#: (`InferenceTimeoutMs`), so the container is already back up when its queue,
#: after the 30 s pause, sends the next one.
INFERENCE_DEADLINE_S = 50

#: ImageNet normalization, the one RF-DETR was trained with.
IMAGENET_MEAN = (0.485, 0.456, 0.406)
IMAGENET_STD = (0.229, 0.224, 0.225)

#: RF-DETR output index -> class. It is the 91-class COCO, with gaps
#: (person=1), and not YOLO's 80: reading one with the other's map swaps a
#: person for a bicycle silently. Copied from `rfdetr.assets.coco_classes`,
#: which doesn't go into the image.
# fmt: off
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
# fmt: on


class BadClip(Exception):
    """The clip doesn't decode to the end. Inspecting the last frame that was
    left would be inspecting another instant, and that is worse than not
    inspecting."""


# ------------------------------------------------------------------ threads


def container_cpu_limit() -> float | None:
    """How many cores the container may use - the compose's `cpus` -, or None
    with no limit.

    It comes from the cgroup v2 `cpu.max`: "quota period", or "max period"
    with no limit. The cgroup path comes from /proc/self/cgroup ("0::/path"),
    which inside a container with its own cgroup namespace is just "/".
    cgroup v1, from an old kernel, isn't read: the compose sets no limit, and
    whoever sets one by hand on such a host gives the number in
    DETECT_THREADS.
    """
    try:
        with open("/proc/self/cgroup") as f:
            path = next(line[3:].strip() for line in f if line.startswith("0::"))
        with open(os.path.join("/sys/fs/cgroup", path.lstrip("/"), "cpu.max")) as f:
            quota, period = f.read().split()
        return None if quota == "max" else int(quota) / int(period)
    except (OSError, StopIteration, ValueError):
        return None


def auto_threads() -> tuple[int, str]:
    """Half the cores the container sees, up to AUTO_THREADS_CAP, and never
    above the container's CPU limit. Returns (threads, where it came from).

    Half, and not all: the other half is left for dwnvr and go2rtc, which can't
    lose a segment. The object detector never uses more cores than threads, and
    that is what keeps it from taking the machine from whoever records.

    The cores come from `sched_getaffinity`, which respects the `cpuset`, and
    NEVER from `os.cpu_count()`, which counts the machine's and not the
    container's. The image is Python 3.12, without `os.process_cpu_count()`. A
    logical core counts as a core, and with hyper-threading half gets it right:
    4 threads on 2 physical cores yield only 8% more than 2, for 85% more CPU.

    The container limit rules because a thread above it is worse than a thread
    less: with `cpus: 2`, 4 threads make the inference as slow as 1 thread,
    spending double the CPU. A fractional limit rounds down.
    """
    cores = len(os.sched_getaffinity(0))
    n, origin = cores // 2, f"half of {cores} core{'s' if cores > 1 else ''}"
    if n > AUTO_THREADS_CAP:
        n, origin = AUTO_THREADS_CAP, f"{cores} cores, up to {AUTO_THREADS_CAP}"
    limit = container_cpu_limit()
    if limit is not None and limit < n:
        n, origin = int(limit), f"container CPU limit, {limit:g}"
    return max(1, n), origin


def configured_threads() -> tuple[int, str]:
    """DETECT_THREADS: `auto`, which applies when it isn't set, or a number from
    1 up. Returns (threads, where it came from)."""
    value = os.environ.get("DETECT_THREADS", "").strip().lower() or "auto"
    if value == "auto":
        n, origin = auto_threads()
        return n, f"auto: {origin}"
    if value.isdecimal() and int(value) >= 1:
        return int(value), "DETECT_THREADS"
    # 0 is refused too: it is onnxruntime's automatic, which counts the
    # MACHINE's physical cores and ignores the container. With `cpus: 2`, on a
    # machine of 14 physical cores, the inference got 16x slower.
    sys.exit(f"DETECT_THREADS={value!r}: use auto or a number from 1 up")


# ------------------------------------------------------------------ the video


def last_frame(fmp4: bytes, threads: int) -> tuple[int, np.ndarray]:
    """Decodes the whole clip and returns (frames decoded, the last one in
    RGB). The clip starts on an I frame, so it decodes on its own."""
    n, last = 0, None
    try:
        with av.open(io.BytesIO(fmp4), format="mp4") as c:
            st = c.streams.video[0]
            st.codec_context.thread_count = threads
            for fr in c.decode(st):
                n, last = n + 1, fr
    except (av.error.FFmpegError, IndexError) as ex:
        raise BadClip(str(ex)[:200]) from ex
    if last is None:
        raise BadClip("no frame in the clip")
    return n, last.to_ndarray(format="rgb24")


def frame_jpeg(img: np.ndarray, width: int, quality: int) -> bytes:
    """Returns the frame as a JPEG, scaled down to `width` keeping the aspect
    ratio.

    Encodes with PyAV, which is already here for the video: a one-frame mjpeg
    container is a .jpg file. The resize is swscale's, in `reformat`.
    """
    src_h, src_w = img.shape[0], img.shape[1]
    width = min(width, src_w)  # never upscale: it invents no detail and only costs bytes
    height = max(2, round(src_h * width / src_w))
    width, height = width - width % 2, height - height % 2

    frame = av.VideoFrame.from_ndarray(img, format="rgb24")
    frame = frame.reformat(width=width, height=height, format="yuvj420p")

    buf = io.BytesIO()
    with av.open(buf, mode="w", format="mjpeg") as c:
        st = c.add_stream("mjpeg", rate=fractions.Fraction(1, 1))
        st.width, st.height, st.pix_fmt = width, height, "yuvj420p"
        # In mjpeg the quality goes through the global quantizer, from 1 (best) to 31.
        st.codec_context.qmin = st.codec_context.qmax = max(1, min(31, round(32 - quality * 31 / 100)))
        for packet in st.encode(frame):
            c.mux(packet)
        for packet in st.encode():
            c.mux(packet)
    return buf.getvalue()


# ------------------------------------------------------------------ preparation


def bilinear(img: np.ndarray, out_h: int, out_w: int) -> np.ndarray:
    """Bilinear WITHOUT antialias, with half-pixel centers - the convention of
    the `F.resize(antialias=False)` that RF-DETR uses. `PIL.BILINEAR` is not
    that: when shrinking it averages over the area, and fidelity drops."""
    h, w = img.shape[:2]
    y = (np.arange(out_h, dtype=np.float32) + 0.5) * (h / out_h) - 0.5
    x = (np.arange(out_w, dtype=np.float32) + 0.5) * (w / out_w) - 0.5
    y0 = np.floor(y).astype(np.int32)
    x0 = np.floor(x).astype(np.int32)
    fy = (y - y0)[:, None, None]
    fx = (x - x0)[None, :, None]
    ya, yb = np.clip(y0, 0, h - 1), np.clip(y0 + 1, 0, h - 1)
    xa, xb = np.clip(x0, 0, w - 1), np.clip(x0 + 1, 0, w - 1)
    f = img.astype(np.float32)
    top = f[ya][:, xa] + (f[ya][:, xb] - f[ya][:, xa]) * fx
    bottom = f[yb][:, xa] + (f[yb][:, xb] - f[yb][:, xa]) * fx
    return (top + (bottom - top) * fy).astype(np.float32, copy=False)


def prepare(img: np.ndarray, in_h: int, in_w: int) -> np.ndarray:
    """Camera frame -> RF-DETR tensor: stretches it to the input, with no
    letterbox (which is what its predict() does), and normalizes by ImageNet."""
    x = img.astype(np.float32) if img.shape[:2] == (in_h, in_w) else bilinear(img, in_h, in_w)
    x = x / 255.0
    x = (x - np.array(IMAGENET_MEAN, dtype=np.float32)) / np.array(IMAGENET_STD, dtype=np.float32)
    return np.ascontiguousarray(x.transpose(2, 0, 1)[None], dtype=np.float32)


# ------------------------------------------------------------------ the model


class Model:
    def __init__(self, path: str, threads: int):
        ort.disable_telemetry_events()
        o = ort.SessionOptions()
        # onnxruntime obeys neither OMP_NUM_THREADS nor the container's CPU
        # limit: its number of threads is this one, and only this one.
        o.intra_op_num_threads = threads
        o.inter_op_num_threads = 1
        self.session = ort.InferenceSession(path, o, providers=["CPUExecutionProvider"])
        input_ = self.session.get_inputs()[0]
        self.input_name = input_.name
        _, _, self.in_h, self.in_w = input_.shape
        self.name = os.path.basename(path)
        self.threads = threads

    def infer(self, img: np.ndarray, min_score: float) -> tuple[list, dict]:
        t0 = time.perf_counter()
        x = prepare(img, self.in_h, self.in_w)
        t1 = time.perf_counter()
        boxes, logits = (s[0] for s in self.session.run(None, {self.input_name: x}))
        t2 = time.perf_counter()

        # (300, 4) in normalized cxcywh, and (300, 91) of logits. Since RF-DETR
        # stretches the frame, the normalized box is ALREADY a fraction of the
        # camera's frame - whatever its resolution.
        conf = 1 / (1 + np.exp(-logits))
        scores, class_ids = conf.max(1), conf.argmax(1)
        detections = []
        for i in np.flatnonzero(scores >= min_score):
            class_name = CLASSES.get(int(class_ids[i]))
            if class_name is None:
                continue  # padding index of the 91-class COCO
            cx, cy, w, h = boxes[i]
            detections.append(
                {
                    "class": class_name,
                    "score": float(scores[i]),
                    "box": [float(cx - w / 2), float(cy - h / 2), float(cx + w / 2), float(cy + h / 2)],
                }
            )
        return detections, {"preprocess": round((t1 - t0) * 1000, 1), "model": round((t2 - t1) * 1000, 1)}


# ------------------------------------------------------------------ the HTTP


def inference_stuck():
    """Exits the whole process. `os._exit`, and not `sys.exit`: this runs on
    the timer's thread, and `sys.exit` would end only it, with the inference
    stuck on the other."""
    print(
        f"inference went past {INFERENCE_DEADLINE_S} s: stuck, exiting for Docker to restart",
        file=sys.stderr,
        flush=True,
    )
    os._exit(1)


def stop_requested(*_):
    """Exits right away on the SIGTERM of `docker stop`. There is no state to
    save: the inference in progress just fails, and dwnvr already handles an
    inference that failed.

    Without this the signal does nothing. In the container this process is PID
    1, and PID 1 ignores every signal it has no handler for: Docker waited the
    10 s timeout and killed it with SIGKILL, on every restart and every `up -d`
    that recreates the container."""
    os._exit(0)


class Handler(http.server.BaseHTTPRequestHandler):
    model: Model  # set in main
    turn = threading.Lock()  # one inference at a time

    def _respond(self, code: int, body: dict):
        b = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)

    def do_GET(self):
        if self.path != "/health":
            return self._respond(404, {"error": "not found"})
        m = self.model
        self._respond(200, {"model": m.name, "inputSize": f"{m.in_w}x{m.in_h}", "threads": m.threads})

    def do_POST(self):
        url = urllib.parse.urlsplit(self.path)
        if url.path != "/detect":
            return self._respond(404, {"error": "not found"})
        query = urllib.parse.parse_qs(url.query)
        try:
            min_score = float(query.get("minScore", [DEFAULT_MIN_SCORE])[0])
            frame_width = min(MAX_FRAME_WIDTH, int(query.get("frameWidth", [0])[0]))
            quality = min(100, max(1, int(query.get("quality", [75])[0])))
        except ValueError:
            return self._respond(400, {"error": "unreadable minScore, frameWidth or quality"})
        size = int(self.headers.get("Content-Length") or 0)
        if size <= 0 or size > MAX_CLIP_BYTES:
            return self._respond(413 if size > 0 else 400, {"error": f"clip of {size} bytes"})
        body = self.rfile.read(size)

        with self.turn:
            watchdog = threading.Timer(INFERENCE_DEADLINE_S, inference_stuck)
            watchdog.daemon = True
            watchdog.start()
            try:
                t0 = time.perf_counter()
                try:
                    n, img = last_frame(body, self.model.threads)
                except BadClip as ex:
                    return self._respond(422, {"error": f"clip doesn't decode: {ex}"})
                t1 = time.perf_counter()
                detections, timings = self.model.infer(img, min_score)
                timings["decode"] = round((t1 - t0) * 1000, 1)

                # Only with a detection: an onset frame with no object nobody
                # sees, and they are most of the inferences. Failing to encode
                # doesn't lose the detection.
                frame = None
                if detections and frame_width > 0:
                    t2 = time.perf_counter()
                    try:
                        frame = base64.b64encode(frame_jpeg(img, frame_width, quality)).decode()
                        timings["frame"] = round((time.perf_counter() - t2) * 1000, 1)
                    except av.error.FFmpegError as ex:
                        print(f"frame didn't encode: {ex}", file=sys.stderr, flush=True)
            finally:
                watchdog.cancel()

        response = {
            "detections": detections,
            "decodedFrames": n,
            "width": int(img.shape[1]),
            "height": int(img.shape[0]),
            "timingsMs": timings,
        }
        if frame:
            response["frame"] = frame
        self._respond(200, response)

    def log_message(self, format, *args):
        pass  # hundreds of requests per hour; the counting is dwnvr's telemetry


class Server(http.server.ThreadingHTTPServer):
    daemon_threads = True

    def handle_error(self, request, client_address):
        # A client that gave up before the response - dwnvr at its deadline, or
        # the healthcheck - isn't a server error, and doesn't deserve a
        # traceback.
        if isinstance(sys.exc_info()[1], (BrokenPipeError, ConnectionResetError, socket.timeout)):
            return
        super().handle_error(request, client_address)


def main():
    # Right at the start, before the model, which takes a few seconds to load
    # on a weak board. Only the imports, which come before this, stay without
    # it: a `docker stop` in the first half second still waits the 10 s.
    signal.signal(signal.SIGTERM, stop_requested)
    n, origin = configured_threads()
    Handler.model = Model(MODEL_PATH, n)
    m = Handler.model
    print(
        f"dwnvr-detect: {m.name}, input {m.in_w}x{m.in_h}, {n} thread(s) ({origin}), port {PORT}",
        file=sys.stderr,
        flush=True,
    )
    Server(("", PORT), Handler).serve_forever()


if __name__ == "__main__":
    main()
