"""Guard of dwnvr-detect: no module compiles from source at runtime.

Python's `site` imports this file before any import of the server: it is the
`sitecustomize`, found through the image's PYTHONPATH. It swaps the loader of
.py files for one that only accepts the .pyc. Without the guard, a .pyc that is
missing, or that doesn't match the .py, Python would silently ignore and
compile the module from source on every start: the image would get slow with
nobody noticing. With it, that becomes an ImportError with the file's path, and
a line in the log.

It holds for every Python that runs in the container without `-S`: the server
and a `docker exec ... python3`. The healthcheck runs with `-S` and doesn't go
through here. To debug a module without a .pyc by hand, `python3 -S`.

Left outside this guard: this file and what Python loads before it (the
`encodings`, for example), which the build checks (the `check` stage of the
Dockerfile, with `check_pyc.py`); and a .py inside a .zip on the PYTHONPATH,
which zipimport compiles without going through here (the image puts none).
The guard catches what the build can't see, such as a changed PYTHONPATH or
PYTHONPYCACHEPREFIX.
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


class PycOnly(SourceFileLoader):
    """Python's .py loader, minus the compilation from source."""

    def source_to_code(self, data, path, *, _optimize=-1):
        reason = f"{path}: no valid .pyc, and this container doesn't compile from source"
        print(f"dwnvr-detect: {reason}", file=sys.stderr, flush=True)
        raise ImportError(reason, path=path)


def install():
    # The same order as Python (extension, source, bytecode), with the source
    # loader swapped.
    guard = FileFinder.path_hook(
        (ExtensionFileLoader, EXTENSION_SUFFIXES),
        (PycOnly, SOURCE_SUFFIXES),
        (SourcelessFileLoader, BYTECODE_SUFFIXES),
    )
    names = [getattr(h, "__name__", "") for h in sys.path_hooks]
    if "path_hook_for_FileFinder" not in names:
        # A Python of another version, which builds the import differently:
        # better to stop here than to go on without the guard thinking it is on.
        raise RuntimeError(f"dwnvr-detect: .pyc guard without the FileFinder hook in {sys.path_hooks}")
    sys.path_hooks[:] = [guard if n == "path_hook_for_FileFinder" else h for h, n in zip(sys.path_hooks, names)]
    # The finders already created for each sys.path folder use the old loader.
    sys.path_importer_cache.clear()


install()
