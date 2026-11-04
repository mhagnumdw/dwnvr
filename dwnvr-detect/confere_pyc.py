"""Confere, no build, o .pyc da imagem final do dwnvr-detect.

    python3 confere_pyc.py <raiz>

Roda no stage `confere` do Dockerfile, com o Python da máquina de build, sobre
os arquivos da imagem final montados em <raiz>, sem executar nada deles, que
podem ser de outra arquitetura. Sai com erro, e o build falha, se:

- algum .py que o servidor pode importar não tem .pyc;
- algum .pyc não é checked-hash da mesma versão do Python, ou o hash gravado
  nele não bate com o .py que a imagem traz. O Python da imagem o ignoraria
  calado e compilaria o fonte a cada subida;
- algum .pyc não tem .py: sobra de outro build.

O que fica sem .pyc de propósito (testes, pip...) é a regex da variável
PYC_FORA, a mesma que o compileall recebe no `-x`.
"""

import importlib.util
import os
import re
import sys

PREFIXO = "/opt/pyc"  # o PYTHONPYCACHEPREFIX da imagem final
STDLIB = f"/usr/local/lib/python{sys.version_info.major}.{sys.version_info.minor}"
FONTES = (STDLIB, "/opt/pacotes", "/app")
SUFIXO = f".{sys.implementation.cache_tag}.pyc"
CHECKED_HASH = 0b11  # flags do cabeçalho: baseado em hash, e confere o .py


def pyc_de(raiz: str, fonte: str) -> str:
    """Onde o Python da imagem procura o .pyc do `fonte` (caminho dentro dela)."""
    pasta, nome = os.path.split(fonte)
    return f"{raiz}{PREFIXO}{pasta}/{nome.removesuffix('.py')}{SUFIXO}"


def problema_do_pyc(pyc: str, fonte: bytes) -> str | None:
    try:
        with open(pyc, "rb") as f:
            cabecalho = f.read(16)
    except FileNotFoundError:
        return "sem .pyc"
    if cabecalho[:4] != importlib.util.MAGIC_NUMBER:
        return "de outra versão do Python"
    if int.from_bytes(cabecalho[4:8], "little") != CHECKED_HASH:
        return "não é checked-hash"
    if cabecalho[8:16] != importlib.util.source_hash(fonte):
        return "hash não bate com o .py"
    return None


def confere(raiz: str) -> tuple[int, list[str]]:
    fora = re.compile(os.environ["PYC_FORA"])
    esperados, problemas = set(), []
    for base in FONTES:
        if not os.path.isdir(raiz + base):
            # Python de versão diferente entre o build e a imagem: o stdlib
            # dela está noutra pasta, e o .pyc daqui seria de outra versão.
            return 0, [f"a imagem não tem {base}, do Python que gerou o .pyc"]
        for pasta, _, arquivos in os.walk(raiz + base):
            for nome in arquivos:
                fonte = os.path.join(pasta, nome)[len(raiz) :]
                if not nome.endswith(".py") or fora.search(fonte):
                    continue
                pyc = pyc_de(raiz, fonte)
                esperados.add(pyc)
                with open(raiz + fonte, "rb") as f:
                    motivo = problema_do_pyc(pyc, f.read())
                if motivo:
                    problemas.append(f"{motivo}: {fonte}")
    for pasta, _, arquivos in os.walk(raiz + PREFIXO):
        for nome in arquivos:
            pyc = os.path.join(pasta, nome)
            if pyc not in esperados:
                problemas.append(f"sem .py: {pyc[len(raiz) :]}")
    return len(esperados), problemas


def main():
    conferidos, problemas = confere(sys.argv[1])
    if problemas:
        print(f"confere_pyc: {len(problemas)} problema(s) no .pyc da imagem:", file=sys.stderr)
        for p in problemas[:20]:
            print(f"  {p}", file=sys.stderr)
        sys.exit(1)
    print(f"confere_pyc: {conferidos} .pyc conferidos")


if __name__ == "__main__":
    main()
