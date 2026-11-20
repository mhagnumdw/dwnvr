"""Checks, at build time, the .pyc of the final dwnvr-detect image.

    python3 check_pyc.py <root>

Runs in the `check` stage of the Dockerfile, with the build machine's Python,
over the final image's files mounted at <root>, without executing anything from
them, since they may be for another architecture. It exits with an error, and
the build fails, if:

- any .py the server can import has no .pyc;
- any .pyc isn't checked-hash of the same Python version, or the hash recorded
  in it doesn't match the .py the image carries. The image's Python would
  silently ignore it and compile the source on every start;
- any .pyc has no .py: a leftover from another build.

What stays without a .pyc on purpose (tests, pip...) is the regex of the
PYC_SKIP variable, the same one compileall gets in `-x`.
"""

import importlib.util
import os
import re
import sys

PREFIX = "/opt/pyc"  # the final image's PYTHONPYCACHEPREFIX
STDLIB = f"/usr/local/lib/python{sys.version_info.major}.{sys.version_info.minor}"
SOURCES = (STDLIB, "/opt/packages", "/app")
SUFFIX = f".{sys.implementation.cache_tag}.pyc"
CHECKED_HASH = 0b11  # header flags: hash-based, and checks the .py


def pyc_of(root: str, source: str) -> str:
    """Where the image's Python looks for the .pyc of `source` (a path inside it)."""
    folder, name = os.path.split(source)
    return f"{root}{PREFIX}{folder}/{name.removesuffix('.py')}{SUFFIX}"


def pyc_problem(pyc: str, source: bytes) -> str | None:
    try:
        with open(pyc, "rb") as f:
            header = f.read(16)
    except FileNotFoundError:
        return "no .pyc"
    if header[:4] != importlib.util.MAGIC_NUMBER:
        return "from another Python version"
    if int.from_bytes(header[4:8], "little") != CHECKED_HASH:
        return "not checked-hash"
    if header[8:16] != importlib.util.source_hash(source):
        return "hash doesn't match the .py"
    return None


def check(root: str) -> tuple[int, list[str]]:
    skip = re.compile(os.environ["PYC_SKIP"])
    expected, problems = set(), []
    for base in SOURCES:
        if not os.path.isdir(root + base):
            # A different Python version between the build and the image: its
            # stdlib is in another folder, and the .pyc from here would be
            # from another version.
            return 0, [f"the image doesn't have {base}, from the Python that generated the .pyc"]
        for folder, _, files in os.walk(root + base):
            for name in files:
                source = os.path.join(folder, name)[len(root) :]
                if not name.endswith(".py") or skip.search(source):
                    continue
                pyc = pyc_of(root, source)
                expected.add(pyc)
                with open(root + source, "rb") as f:
                    reason = pyc_problem(pyc, f.read())
                if reason:
                    problems.append(f"{reason}: {source}")
    for folder, _, files in os.walk(root + PREFIX):
        for name in files:
            pyc = os.path.join(folder, name)
            if pyc not in expected:
                problems.append(f"no .py: {pyc[len(root) :]}")
    return len(expected), problems


def main():
    checked, problems = check(sys.argv[1])
    if problems:
        print(f"check_pyc: {len(problems)} problem(s) in the image's .pyc:", file=sys.stderr)
        for p in problems[:20]:
            print(f"  {p}", file=sys.stderr)
        sys.exit(1)
    print(f"check_pyc: {checked} .pyc checked")


if __name__ == "__main__":
    main()
