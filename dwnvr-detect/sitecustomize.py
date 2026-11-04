"""Trava do dwnvr-detect: nenhum módulo compila do fonte em runtime.

O `site` do Python importa este arquivo antes de qualquer import do servidor:
é o `sitecustomize`, achado pelo PYTHONPATH da imagem. Ele troca o loader dos
arquivos .py por um que só aceita o .pyc. Sem a trava, o .pyc que falta, ou que
não bate com o .py, o Python ignoraria calado e compilaria o módulo do fonte a
cada subida: a imagem ficaria lenta sem ninguém ver. Com ela, isso vira
ImportError com o caminho do arquivo, e uma linha no log.

Vale para todo Python que roda no container sem `-S`: o servidor e um
`docker exec ... python3`. O healthcheck roda com `-S` e não passa por aqui.
Para depurar à mão um módulo sem .pyc, `python3 -S`.

O build confere os .pyc antes (o stage `confere` do Dockerfile, com o
`confere_pyc.py`). Esta trava pega o que o build não vê, como um PYTHONPATH ou
um PYTHONPYCACHEPREFIX trocado.
"""

import sys
from importlib.machinery import (
    BYTECODE_SUFFIXES,
    EXTENSION_SUFFIXES,
    SOURCE_SUFFIXES,
    ExtensionFileLoader,
    FileFinder,
    SourceFileLoader,
    SourcelessFileLoader,
)


class SoDoPyc(SourceFileLoader):
    """O loader de .py do Python, menos a compilação do fonte."""

    def source_to_code(self, data, path, *, _optimize=-1):
        motivo = f"{path}: sem .pyc válido, e este container não compila do fonte"
        print(f"dwnvr-detect: {motivo}", file=sys.stderr, flush=True)
        raise ImportError(motivo, path=path)


def instala():
    # A mesma ordem do Python (extensão, fonte, bytecode), com o loader do
    # fonte trocado.
    trava = FileFinder.path_hook(
        (ExtensionFileLoader, EXTENSION_SUFFIXES),
        (SoDoPyc, SOURCE_SUFFIXES),
        (SourcelessFileLoader, BYTECODE_SUFFIXES),
    )
    nomes = [getattr(h, "__name__", "") for h in sys.path_hooks]
    if "path_hook_for_FileFinder" not in nomes:
        # Python de outra versão, que monta o import de outro jeito: melhor
        # parar aqui do que seguir sem a trava achando que ela está ligada.
        raise RuntimeError(f"dwnvr-detect: trava do .pyc sem o hook do FileFinder em {sys.path_hooks}")
    sys.path_hooks[:] = [trava if n == "path_hook_for_FileFinder" else h for h, n in zip(sys.path_hooks, nomes)]
    # Os finders já criados para cada pasta do sys.path usam o loader antigo.
    sys.path_importer_cache.clear()


instala()
