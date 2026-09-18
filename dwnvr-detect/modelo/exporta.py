#!/usr/bin/env python3
"""Gera o `.onnx` do dwnvr-detect: o RF-DETR Nano a 512x288, em int8.

Ferramenta de quem desenvolve o dwnvr. Quem só usa recebe o `.onnx` pronto
dentro da imagem e nunca roda isto.

O aparelho não roda `.pt`: não tem torch, nem RAM para ele. O que roda é um
grafo congelado, e é isso que sai daqui, em dois passos:

  fp32 - o grafo exportado direto do torch, pelo próprio `rfdetr`
  int8 - o mesmo grafo com pesos e ativações quantizados, CALIBRADO nas
         imagens da pasta `--imagens`

Quantizar é escolher, para cada tensor da rede, a faixa numérica que ele vai
representar, e quem escolhe são as imagens de calibração. A calibração não
ensina nada ao modelo, mas imagem errada dá faixa errada: calibrado só em foto
diurna, o int8 trata o ruído de infravermelho como estouro de escala e perde
objeto justamente à noite. O README ao lado diz como montar a pasta.

O tensor de entrada chega PRONTO: esticar e normalizar ficam fora do grafo, no
`prepara` do `servidor.py`. Este script o importa de lá, para que a calibração
use exatamente o preparo que roda em produção.

  pip install -r requirements.txt
  python exporta.py --imagens ~/calibracao
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

AQUI = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(AQUI.parent))
from servidor import prepara  # noqa: E402

# (altura, largura). As duas precisam ser múltiplas de 32, o que a janela de
# atenção do RF-DETR exige (patch 16 x 2 janelas). 512x288 é 16:9, o formato
# da câmera de segurança comum, e custa ~0,64 do que custaria 640x360.
ENTRADA = (288, 512)

OPSET = 17  # o padrão do rfdetr.export()
LOTE = 1    # o dwnvr olha UM quadro por onset; lote maior não existe aqui

# Ativação sem sinal e peso com sinal, por canal: é o par para o qual o kernel
# ARM64 do onnxruntime tem caminho rápido. O Cortex-A53 não tem dotprod nem
# i8mm, então o int8 ganha ~1,4x, e não os 3 a 4x de uma CPU nova.
ATIVACAO_INT8 = "QUInt8"
PESO_INT8 = "QInt8"
PESO_POR_CANAL = True

# MinMax guarda só o mínimo e o máximo de cada tensor. Os métodos por
# histograma (Percentile, Entropy) guardam TODAS as ativações de todas as
# imagens: com 163 imagens passam de 8 GB de RAM em poucos minutos.
METODO_DE_CALIBRACAO = "MinMax"

# Só Conv e Gemm entram no int8.
#
# `Gemm` é onde mora o peso do RF-DETR: 91,5% dos 108 MB. Sem ele o int8 sai
# com 103 MB, e não encolhe nada. Com ele sai com ~30 MB.
#
# `MatMul` fica DE FORA. Os MatMul do RF-DETR são a atenção, que multiplica
# ativação por ativação, e o kernel ARM do onnxruntime recusa o ponto-zero
# deles: o modelo carrega, roda e morre no primeiro quadro, dentro do decoder.
# Eles guardam 0,3% do peso, então ficar em float não custa tamanho.
OPS_QUANTIZADAS = ("Conv", "Gemm")

EXTENSOES = {".jpg", ".jpeg", ".png"}


def exporta_fp32(destino: pathlib.Path) -> None:
    """Exporta o RF-DETR Nano do COCO. Os pesos são baixados pelo `rfdetr` na
    primeira vez."""
    import rfdetr

    h, w = ENTRADA
    m = rfdetr.RFDETRNano()
    tmp = destino.parent / f".tmp-export-{w}x{h}"
    tmp.mkdir(parents=True, exist_ok=True)
    m.export(output_dir=str(tmp), shape=(h, w), batch_size=LOTE,
             opset_version=OPSET, format="onnx", verbose=False)
    saiu = sorted(tmp.rglob("*.onnx"), key=lambda p: -p.stat().st_size)
    if not saiu:
        raise SystemExit(f"o export não deixou .onnx em {tmp}")
    shutil.move(str(saiu[0]), destino)
    shutil.rmtree(tmp, ignore_errors=True)


class Calibracao:
    """Entrega as imagens, uma a uma, já no formato que o grafo espera. Lê do
    disco na hora, para a RAM não crescer com o tamanho da pasta."""

    def __init__(self, imagens: list[pathlib.Path], nome_da_entrada: str):
        self.imagens = imagens
        self.nome = nome_da_entrada
        self.i = 0

    def get_next(self):
        if self.i >= len(self.imagens):
            return None
        with Image.open(self.imagens[self.i]) as im:
            rgb = np.asarray(im.convert("RGB"))
        self.i += 1
        return {self.nome: prepara(rgb, *ENTRADA)}

    def rewind(self):
        self.i = 0


def quantiza(fp32: pathlib.Path, destino: pathlib.Path, imagens: list[pathlib.Path]) -> None:
    import onnxruntime as ort
    from onnxruntime.quantization import (CalibrationMethod, QuantFormat,
                                          QuantType, quantize_static)
    from onnxruntime.quantization.shape_inference import quant_pre_process

    pronto = destino.parent / f".pre-{destino.name}"
    quant_pre_process(str(fp32), str(pronto), skip_symbolic_shape=False)

    sessao = ort.InferenceSession(str(pronto), providers=["CPUExecutionProvider"])
    entrada = sessao.get_inputs()[0].name
    del sessao

    quantize_static(
        str(pronto), str(destino),
        Calibracao(imagens, entrada),
        quant_format=QuantFormat.QDQ,
        per_channel=PESO_POR_CANAL,
        activation_type=getattr(QuantType, ATIVACAO_INT8),
        weight_type=getattr(QuantType, PESO_INT8),
        calibrate_method=getattr(CalibrationMethod, METODO_DE_CALIBRACAO),
        op_types_to_quantize=list(OPS_QUANTIZADAS),
    )
    pronto.unlink(missing_ok=True)


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--imagens", required=True, type=pathlib.Path,
                    help="pasta com as imagens de calibração (.jpg ou .png)")
    ap.add_argument("--saida", default=AQUI, type=pathlib.Path,
                    help="onde gravar os .onnx (padrão: esta pasta)")
    a = ap.parse_args()

    imagens = sorted(p for p in a.imagens.iterdir() if p.suffix.lower() in EXTENSOES)
    if not imagens:
        raise SystemExit(f"nenhuma imagem .jpg ou .png em {a.imagens}")
    print(f"calibração: {len(imagens)} imagens de {a.imagens}")

    h, w = ENTRADA
    a.saida.mkdir(parents=True, exist_ok=True)
    fp32 = a.saida / f"rfdetr-n_{w}x{h}_fp32.onnx"
    int8 = a.saida / f"rfdetr-n_{w}x{h}_int8.onnx"

    t = time.time()
    # O rfdetr fala muito no stdout; o que interessa é o tamanho e o tempo.
    with contextlib.redirect_stdout(io.StringIO()):
        exporta_fp32(fp32)
    print(f"{fp32.name}  {fp32.stat().st_size / 1e6:6.1f} MB  {time.time() - t:5.1f}s")

    t = time.time()
    with contextlib.redirect_stdout(io.StringIO()):
        quantiza(fp32, int8, imagens)
    print(f"{int8.name}  {int8.stat().st_size / 1e6:6.1f} MB  {time.time() - t:5.1f}s")


if __name__ == "__main__":
    main()
