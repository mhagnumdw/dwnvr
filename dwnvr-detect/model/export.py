#!/usr/bin/env python3
"""Generates the dwnvr-detect `.onnx`: RF-DETR Nano at 512x288, in int8.

A tool for whoever develops dwnvr. Whoever only uses it gets the ready `.onnx`
inside the image and never runs this.

The device doesn't run a `.pt`: it has no torch, nor the RAM for it. What runs
is a frozen graph, and that is what comes out of here, in two steps:

  fp32 - the graph exported straight from torch, by `rfdetr` itself
  int8 - the same graph with weights and activations quantized, CALIBRATED on
         the images of the `--images` folder

Quantizing is choosing, for each tensor of the network, the numeric range it
will represent, and the calibration images are what choose. The calibration
teaches the model nothing, but a wrong image gives a wrong range: calibrated
only on daytime photos, the int8 treats infrared noise as scale overflow and
loses objects precisely at night. The README next to it says how to put the
folder together.

The input tensor arrives READY: stretching and normalizing stay outside the
graph, in the `prepare` of `server.py`. This script imports it from there, so
the calibration uses exactly the preparation that runs in production.

  pip install -r requirements.txt
  python export.py --images ~/calibration
"""

from __future__ import annotations

import argparse
import contextlib
import io
import pathlib
import shutil
import sys
import time

import numpy as np
from PIL import Image

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
from server import prepare

# (height, width). Both must be multiples of 32, which RF-DETR's attention
# window requires (patch 16 x 2 windows). 512x288 is 16:9, the format of the
# usual security camera, and costs ~0.64 of what 640x360 would.
INPUT_SIZE = (288, 512)

OPSET = 17  # rfdetr.export()'s default
BATCH = 1  # dwnvr inspects ONE frame per onset; a larger batch doesn't exist here

# Unsigned activation and signed weight, per channel: it is the pair onnxruntime's
# ARM64 kernel has a fast path for. The Cortex-A53 has neither dotprod nor
# i8mm, so the int8 gains ~1.4x, and not the 3 to 4x of a new CPU.
ACTIVATION_INT8 = "QUInt8"
WEIGHT_INT8 = "QInt8"
WEIGHT_PER_CHANNEL = True

# MinMax keeps only the minimum and maximum of each tensor. The histogram
# methods (Percentile, Entropy) keep ALL the activations of all the images:
# with 163 images they go past 8 GB of RAM in a few minutes.
CALIBRATION_METHOD = "MinMax"

# Only Conv and Gemm go into the int8.
#
# `Gemm` is where RF-DETR's weight lives: 91.5% of the 108 MB. Without it the
# int8 comes out at 103 MB, and shrinks nothing. With it, ~30 MB.
#
# `MatMul` stays OUT. RF-DETR's MatMuls are the attention, which multiplies
# activation by activation, and onnxruntime's ARM kernel refuses their
# zero-point: the model loads, runs and dies on the first frame, inside the
# decoder. They hold 0.3% of the weight, so staying in float costs no size.
QUANTIZED_OPS = ("Conv", "Gemm")

EXTENSIONS = {".jpg", ".jpeg", ".png"}


def export_fp32(target: pathlib.Path) -> None:
    """Exports the COCO RF-DETR Nano. `rfdetr` downloads the weights the first
    time."""
    import rfdetr

    h, w = INPUT_SIZE
    m = rfdetr.RFDETRNano()
    tmp = target.parent / f".tmp-export-{w}x{h}"
    tmp.mkdir(parents=True, exist_ok=True)
    m.export(output_dir=str(tmp), shape=(h, w), batch_size=BATCH, opset_version=OPSET, format="onnx", verbose=False)
    exported = sorted(tmp.rglob("*.onnx"), key=lambda p: -p.stat().st_size)
    if not exported:
        raise SystemExit(f"the export left no .onnx in {tmp}")
    shutil.move(str(exported[0]), target)
    shutil.rmtree(tmp, ignore_errors=True)


class Calibration:
    """Hands over the images one by one, already in the format the graph
    expects. Reads from disk on the spot, so the RAM doesn't grow with the size
    of the folder."""

    def __init__(self, images: list[pathlib.Path], input_name: str):
        self.images = images
        self.name = input_name
        self.i = 0

    def get_next(self):
        if self.i >= len(self.images):
            return None
        with Image.open(self.images[self.i]) as im:
            rgb = np.asarray(im.convert("RGB"))
        self.i += 1
        return {self.name: prepare(rgb, *INPUT_SIZE)}

    def rewind(self):
        self.i = 0


def quantize(fp32: pathlib.Path, target: pathlib.Path, images: list[pathlib.Path]) -> None:
    import onnxruntime as ort
    from onnxruntime.quantization import CalibrationMethod, QuantFormat, QuantType, quantize_static
    from onnxruntime.quantization.shape_inference import quant_pre_process

    prepared = target.parent / f".pre-{target.name}"
    quant_pre_process(str(fp32), str(prepared), skip_symbolic_shape=False)

    session = ort.InferenceSession(str(prepared), providers=["CPUExecutionProvider"])
    input_name = session.get_inputs()[0].name
    del session

    quantize_static(
        str(prepared),
        str(target),
        Calibration(images, input_name),
        quant_format=QuantFormat.QDQ,
        per_channel=WEIGHT_PER_CHANNEL,
        activation_type=getattr(QuantType, ACTIVATION_INT8),
        weight_type=getattr(QuantType, WEIGHT_INT8),
        calibrate_method=getattr(CalibrationMethod, CALIBRATION_METHOD),
        op_types_to_quantize=list(QUANTIZED_OPS),
    )
    prepared.unlink(missing_ok=True)


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument(
        "--images", required=True, type=pathlib.Path, help="folder with the calibration images (.jpg or .png)"
    )
    ap.add_argument(
        "--output", default=HERE, type=pathlib.Path, help="where to write the .onnx files (default: this folder)"
    )
    a = ap.parse_args()

    images = sorted(p for p in a.images.iterdir() if p.suffix.lower() in EXTENSIONS)
    if not images:
        raise SystemExit(f"no .jpg or .png image in {a.images}")
    print(f"calibration: {len(images)} images from {a.images}")

    h, w = INPUT_SIZE
    a.output.mkdir(parents=True, exist_ok=True)
    fp32 = a.output / f"rfdetr-n_{w}x{h}_fp32.onnx"
    int8 = a.output / f"rfdetr-n_{w}x{h}_int8.onnx"

    t = time.time()
    # rfdetr talks a lot on stdout; what matters is the size and the time.
    with contextlib.redirect_stdout(io.StringIO()):
        export_fp32(fp32)
    print(f"{fp32.name}  {fp32.stat().st_size / 1e6:6.1f} MB  {time.time() - t:5.1f}s")

    t = time.time()
    with contextlib.redirect_stdout(io.StringIO()):
        quantize(fp32, int8, images)
    print(f"{int8.name}  {int8.stat().st_size / 1e6:6.1f} MB  {time.time() - t:5.1f}s")


if __name__ == "__main__":
    main()
