# The dwnvr-detect model

`rfdetr-n_512x288_int8.onnx` is the model that goes inside the `dwnvr-detect`
image. This folder is for whoever develops dwnvr: whoever just uses it gets the
finished file and never runs anything from here.

- `rfdetr-n_512x288_int8.onnx` - the published model (~30 MB)
- `export.py` - generates the `.onnx` again, from the public weights
- `requirements.txt` - the versions that rebuild the published one
- `LICENSE-RF-DETR` - the weights' license

## Where it comes from

1. **The weights** are those of RF-DETR Nano trained on COCO, from Roboflow,
   under Apache-2.0 (see `LICENSE-RF-DETR`). What the model can recognize is
   theirs; dwnvr trains nothing.
2. **`export.py` exports** the weights to ONNX with the input fixed at
   512x288.
3. **And quantizes to int8**, calibrating on the images of a folder.

Against the fp32 of the same model, int8 loses 2 points of accuracy and runs 1.4x
faster on an Orange Pi Zero 3. The size drops from ~108 MB to ~30 MB.

## Calibration

Quantizing is choosing, for each tensor of the network, the numeric range it
will represent. The calibration images are what choose it. They teach the model
nothing, but a wrong image gives a wrong range: calibrated only with daytime
photos, int8 treats infrared noise as a scale overflow and misses objects
precisely at night, which is half the hours of an NVR.

That is why the sample is built like this:

- **split among the cameras**, in equal parts, because the model is one for all
  of them
- **half day and half night**, because at night the image changes nature:
  infrared, black and white, grainy
- **half with an object and half without**. A still scene is real input for the
  object detector: a good part of the motions it looks at is shadow, rain or
  leaf

The published `.onnx` was calibrated with 163 frames from the author's cameras,
at their native size (640x360). Those frames are not in the repository.

## Generating it again

On any machine, without a GPU. `rfdetr` downloads the weights the first time,
and the process reaches ~3 GB of RAM:

```sh
cd dwnvr-detect/model
python3.12 -m venv .venv
.venv/bin/pip install -r requirements.txt
.venv/bin/python export.py --images ~/calibration
```

The `--images` folder has `.jpg` or `.png`, of any resolution. The script writes
the fp32 and the int8 in this folder, over the published one. The fp32 stays out
of git and of the image build.

With the calibration frames of the published one, the int8 comes out
byte-for-byte identical to it:

```text
926bc9a61fe63ea8624e18859300a7c5a1768108d556c9bad9492620d3913e29  rfdetr-n_512x288_int8.onnx
```

That requires the PyPI `torch`, which is the CUDA build (~5 GB of packages),
even without a GPU. The CPU one (`--extra-index-url https://download.pytorch.org/whl/cpu`)
is much smaller and gives an int8 of the same quality, but a different file: its
fp32 differs in the last bit of one tensor, and quantization amplifies that
into ~17% of the findings swapped at the 0.20 floor. Against the fp32, the two
int8s tie (see
[`TODO_recalibrar-modelo-com-imagens-publicas.md`](../../docs/TODO/TODO_recalibrar-modelo-com-imagens-publicas.md)).

The image preparation, which stretches to 512x288 and normalizes by ImageNet, is
not a copy: `export.py` imports `prepare` from `server.py`. That way the
calibration uses, by construction, the same preparation that runs in production.

## Using the fp32 instead of the int8

The fp32 goes through no calibration at all, so it serves as a comparison when
the int8 seems to be wrong on a camera. It goes in without rebuilding the image,
through a volume and `DETECT_MODEL`:

```yaml
    volumes:
      - ./rfdetr-n_512x288_fp32.onnx:/models/fp32.onnx:ro
    environment:
      - DETECT_MODEL=/models/fp32.onnx
```

Any `.onnx` with the same output as RF-DETR works: the server reads the input
size from the file itself.
