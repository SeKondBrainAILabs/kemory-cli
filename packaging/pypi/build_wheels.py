#!/usr/bin/env python3
"""Build platform wheels that carry the native kemory binary.

`uvx kemory` and `pip install kemory` install a wheel; this turns each
GoReleaser archive into one, with the binary as the wheel's only script. The
package has no Python code and no dependencies.

    python3 packaging/pypi/build_wheels.py --version 0.7.0 --dist dist --out dist/wheels

Standard library only, so the release job needs nothing installed.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import io
import tarfile
import zipfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent

# GoReleaser archive suffix -> wheel platform tags. Go binaries are static
# (CGO_ENABLED=0), so one Linux wheel serves both glibc and musl. Go 1.25+
# needs macOS 12 or later.
TARGETS = {
    "macos-arm64": ["macosx_12_0_arm64"],
    "macos-x64": ["macosx_12_0_x86_64"],
    "linux-x64": ["manylinux_2_17_x86_64", "manylinux2014_x86_64", "musllinux_1_1_x86_64"],
    "linux-arm64": ["manylinux_2_17_aarch64", "manylinux2014_aarch64", "musllinux_1_1_aarch64"],
    "windows-x64": ["win_amd64"],
}

SUMMARY = "Kemory CLI: connect Claude Code, Cursor and other MCP clients to your Kemory memory vault"


def read_binary(archive: Path) -> tuple[str, bytes]:
    """Return the binary's name and bytes. Only that one member is read."""
    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as zf:
            return "kemory.exe", zf.read("kemory.exe")
    with tarfile.open(archive, "r:gz") as tf:
        member = tf.getmember("kemory")
        if not member.isfile():
            raise SystemExit(f"{archive}: kemory is not a regular file")
        data = tf.extractfile(member)
        assert data is not None
        return "kemory", data.read()


def record_line(path: str, data: bytes) -> str:
    digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).rstrip(b"=").decode()
    return f"{path},sha256={digest},{len(data)}"


def metadata(version: str) -> bytes:
    readme = (HERE / "README.md").read_text(encoding="utf-8")
    headers = [
        "Metadata-Version: 2.4",
        "Name: kemory",
        f"Version: {version}",
        f"Summary: {SUMMARY}",
        "Author-email: SeKondBrain Engineering <engineering@sekondbrain.ai>",
        "License-Expression: MIT",
        "License-File: LICENSE",
        "Project-URL: Homepage, https://kemory.sekondbrain.ai",
        "Project-URL: Source, https://github.com/SeKondBrainAILabs/kemory-cli",
        "Project-URL: Issues, https://github.com/SeKondBrainAILabs/kemory-cli/issues",
        "Keywords: kemory,memory,mcp,ai,agents,claude",
        "Classifier: Environment :: Console",
        "Classifier: Operating System :: MacOS",
        "Classifier: Operating System :: POSIX :: Linux",
        "Classifier: Operating System :: Microsoft :: Windows",
        "Description-Content-Type: text/markdown",
    ]
    return ("\n".join(headers) + "\n\n" + readme).encode()


def build(version: str, target: str, archive: Path, out: Path) -> Path:
    tags = TARGETS[target]
    name, binary = read_binary(archive)
    dist_info = f"kemory-{version}.dist-info"
    files: list[tuple[str, bytes, int]] = [
        (f"kemory-{version}.data/scripts/{name}", binary, 0o755),
        (f"{dist_info}/METADATA", metadata(version), 0o644),
        (
            f"{dist_info}/WHEEL",
            (
                "Wheel-Version: 1.0\nGenerator: kemory-cli build_wheels.py\nRoot-Is-Purelib: false\n"
                + "".join(f"Tag: py3-none-{t}\n" for t in tags)
            ).encode(),
            0o644,
        ),
        (f"{dist_info}/licenses/LICENSE", (ROOT / "LICENSE").read_bytes(), 0o644),
    ]
    record = [record_line(path, data) for path, data, _ in files] + [f"{dist_info}/RECORD,,"]
    files.append((f"{dist_info}/RECORD", ("\n".join(record) + "\n").encode(), 0o644))

    wheel = out / f"kemory-{version}-py3-none-{'.'.join(tags)}.whl"
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", compression=zipfile.ZIP_DEFLATED) as zf:
        for path, data, mode in files:
            info = zipfile.ZipInfo(path, date_time=(2020, 2, 2, 0, 0, 0))  # reproducible
            info.external_attr = (0o100000 | mode) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            zf.writestr(info, data)
    wheel.write_bytes(buf.getvalue())
    return wheel


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--version", required=True)
    parser.add_argument("--dist", type=Path, default=ROOT / "dist")
    parser.add_argument("--out", type=Path, default=ROOT / "dist" / "wheels")
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    for target in TARGETS:
        ext = "zip" if target.startswith("windows") else "tar.gz"
        archive = args.dist / f"kemory-{target}.{ext}"
        if not archive.exists():
            raise SystemExit(f"missing {archive}")
        print(build(args.version, target, archive, args.out))


if __name__ == "__main__":
    main()
