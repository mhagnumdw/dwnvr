# O modelo do dwnvr-detect

`rfdetr-n_512x288_int8.onnx` é o modelo que vai dentro da imagem do
`dwnvr-detect`. Esta pasta é de quem desenvolve o dwnvr: quem só usa recebe o
arquivo pronto e nunca roda nada daqui.

- `rfdetr-n_512x288_int8.onnx` - o modelo publicado (~30 MB)
- `exporta.py` - gera o `.onnx` de novo, a partir dos pesos públicos
- `requirements.txt` - as versões que geraram o publicado
- `LICENSE-RF-DETR` - a licença dos pesos

## De onde ele vem

1. **Os pesos** são os do RF-DETR Nano treinado no COCO, da Roboflow, sob
   Apache-2.0 (ver `LICENSE-RF-DETR`). É deles o que o modelo sabe reconhecer;
   o dwnvr não treina nada.
2. **O `exporta.py` exporta** os pesos para ONNX com a entrada fixa em
   512x288.
3. **E quantiza para int8**, calibrando nas imagens de uma pasta.

Contra o fp32 do mesmo modelo, o int8 perde 2 pontos de acerto e roda 1,4x
mais rápido num Orange Pi Zero 3. O tamanho cai de ~108 MB para ~30 MB.

## A calibração

Quantizar é escolher, para cada tensor da rede, a faixa numérica que ele vai
representar. Quem escolhe são as imagens de calibração. Elas não ensinam nada
ao modelo, mas imagem errada dá faixa errada: calibrado só com foto diurna, o
int8 trata o ruído do infravermelho como estouro de escala e perde objeto
justamente à noite, que é metade das horas de um NVR.

Por isso a amostra é montada assim:

- **dividida entre as câmeras**, em partes iguais, porque o modelo é um só
  para todas
- **metade de dia e metade de noite**, porque à noite a imagem muda de
  natureza: infravermelho, preto e branco, granulada
- **metade com objeto e metade sem**. Cena parada é entrada real do detector:
  boa parte dos movimentos que ele olha é sombra, chuva ou folha

O `.onnx` publicado foi calibrado com 163 quadros das câmeras do autor, no
tamanho nativo delas (640x360). Esses quadros não estão no repositório.

## Gerar de novo

Numa máquina qualquer, sem GPU. O `rfdetr` baixa os pesos na primeira vez, e
o processo chega a ~3 GB de RAM:

```sh
cd dwnvr-detect/modelo
python3.12 -m venv .venv
.venv/bin/pip install -r requirements.txt --extra-index-url https://download.pytorch.org/whl/cpu
.venv/bin/python exporta.py --imagens ~/calibracao
```

A pasta `--imagens` tem `.jpg` ou `.png`, de qualquer resolução. O script
grava o fp32 e o int8 nesta pasta, por cima do publicado. O fp32 fica fora do
git e do build da imagem.

O preparo das imagens, que estica até 512x288 e normaliza pelo ImageNet, não é
uma cópia: o `exporta.py` importa o `prepara` do `servidor.py`. Assim a
calibração usa por construção o mesmo preparo que roda em produção.

## Usar o fp32 no lugar do int8

O fp32 não passa por calibração nenhuma, então serve de comparação quando o
int8 parecer errar numa câmera. Ele entra sem rebuild da imagem, por volume e
`DETECT_MODELO`:

```yaml
    volumes:
      - ./rfdetr-n_512x288_fp32.onnx:/modelos/fp32.onnx:ro
    environment:
      - DETECT_MODELO=/modelos/fp32.onnx
```

Qualquer `.onnx` com a mesma saída do RF-DETR funciona: o servidor lê o
tamanho da entrada do próprio arquivo.
