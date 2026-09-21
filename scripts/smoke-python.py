"""Build and install the Python wheel, then use it outside this checkout."""

import os
import shutil
import subprocess
import sys
import tempfile
import tomllib
import zipfile
from email.parser import BytesParser
from pathlib import Path

from packaging.requirements import Requirement
from packaging.version import Version

ROOT = Path(__file__).resolve().parents[1]
CONSUMER = r"""
import asyncio
from importlib.metadata import version
from pathlib import Path
import sys
import nightseam.duplex
import nightseam.runtime
from nightseam.duplex import at, mount
from nightseam.duplex.websocket import dial, listen
from nightseam.runtime import Peer, call_wire, forward_wire, handle_wire, wire_pair

checkout = Path(sys.argv[1]).resolve()
assert version("nightseam") == sys.argv[2], version("nightseam")
for module in (nightseam.duplex, nightseam.runtime):
    assert not Path(module.__file__).resolve().is_relative_to(checkout), module.__file__

async def main():
    listener = await listen()
    connection = await dial(listener.url)
    server = Peer(await listener.accept(), "server")
    server.handle("echo", lambda value, context: value)
    client = Peer(connection)
    access, binding = wire_pair()
    handle_wire(binding, ["echo"], lambda value, context: value)
    detach = forward_wire(server.wire(), access)
    try:
        assert await client.call("echo", {"text": "hello", "nullable": None}) == {"text": "hello", "nullable": None}
        selected = at(mount({"service": client.wire()}), ["service"])
        assert await call_wire(selected, ["echo"], {"wire": True}) == {"wire": True}
    finally:
        detach()
        access.close()
        await client.close()
        await server.close()
        await listener.close()
async def bounded():
    async with asyncio.timeout(30):
        await main()
asyncio.run(bounded())
print("Python wheel: isolated imports, peer and composed Wire WebSocket round trips passed")
"""


def main(root=ROOT):
    root = Path(root).resolve()
    env = {key: value for key, value in os.environ.items() if key not in ("PYTHONPATH", "PYTHONHOME")}
    env["PYTHONNOUSERSITE"] = "1"
    with tempfile.TemporaryDirectory(prefix="nightseam-python-wheel-") as directory:
        scratch = Path(directory).resolve()
        if scratch.is_relative_to(root):
            raise RuntimeError("the wheel smoke needs a temporary directory outside the checkout")
        source = scratch / "source"
        source.mkdir()
        project = tomllib.loads((root / "pyproject.toml").read_text(encoding="utf8"))
        # Build only a copy, so setuptools cannot leave build/ or egg-info in
        # a checkout another gate is using, and stale build output cannot pass.
        for name in ("pyproject.toml", "LICENSE", "NOTICE"):
            shutil.copy2(root / name, source / name)
        required = set()
        for package, directory in project["tool"]["setuptools"]["package-dir"].items():
            package_source = (root / directory).resolve()
            if not package_source.is_relative_to(root):
                raise RuntimeError("package source is outside the checkout: " + directory)
            shutil.copytree(package_source, source / directory, ignore=shutil.ignore_patterns("__pycache__", "*.pyc"))
            required.update(
                package.replace(".", "/") + "/" + path.relative_to(package_source).as_posix()
                for path in package_source.rglob("*.py")
            )
        wheels = scratch / "wheels"
        subprocess.run(
            [sys.executable, "-m", "build", "--wheel", "--outdir", str(wheels), str(source)],
            check=True,
            env=env,
            cwd=scratch,
        )
        (wheel,) = wheels.glob("*.whl")
        with zipfile.ZipFile(wheel) as archive:
            names = set(archive.namelist())
            for name in sorted(required):
                if name not in names:
                    raise RuntimeError("wheel omits " + name)
            for notice in ("LICENSE", "NOTICE"):
                if not any(name.endswith("/licenses/" + notice) for name in names):
                    raise RuntimeError("wheel omits " + notice)
            (metadata_name,) = (name for name in names if name.endswith(".dist-info/METADATA"))
            metadata = BytesParser().parsebytes(archive.read(metadata_name))
            expected_version = str(Version(project["project"]["version"]))
            if metadata["Name"] != "nightseam" or metadata["Version"] != expected_version:
                raise RuntimeError("wheel name/version does not match pyproject.toml")
            if metadata["Requires-Python"] != project["project"]["requires-python"]:
                raise RuntimeError("wheel does not retain the supported Python version")
            dependencies = {Requirement(value) for value in metadata.get_all("Requires-Dist", [])}
            if not {Requirement(value) for value in project["project"]["dependencies"]} <= dependencies:
                raise RuntimeError("wheel omits a runtime dependency")
        consumer = scratch / "consumer"
        subprocess.run([sys.executable, "-m", "venv", str(consumer)], check=True, env=env, cwd=scratch)
        python = consumer / ("Scripts/python.exe" if os.name == "nt" else "bin/python")
        subprocess.run(
            [str(python), "-m", "pip", "install", "--disable-pip-version-check", str(wheel)],
            check=True,
            env=env,
            cwd=consumer,
        )
        subprocess.run(
            [str(python), "-I", "-c", CONSUMER, str(root), expected_version],
            check=True,
            env=env,
            cwd=consumer,
            timeout=45,
        )


if __name__ == "__main__":
    main()
