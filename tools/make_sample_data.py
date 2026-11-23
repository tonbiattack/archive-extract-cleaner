#!/usr/bin/env python3
"""ストア掲載用スクリーンショットのサンプルデータを作る。

個人名・実在のファイル名・機密情報を含めないため、架空の写真整理データを
生成する。ZIP と解凍済みフォルダの組を、判定が一通り揃うように並べる。

使い方:
    python tools/make_sample_data.py <出力先ディレクトリ>
"""

from __future__ import annotations

import random
import sys
import zipfile
from pathlib import Path

# 架空のアルバム名。実在の人物・作品と関係しない。
ALBUMS = [
    ("sample-album-01", 12),
    ("sample-album-02", 8),
    ("sample-album-03", 15),
    ("sample-album-04", 6),
    ("sample-album-05", 10),
]

# 判定を一通り出すための組。名前は用途が分かる程度に留める。
CASES = [
    # (名前, ZIPを作る, フォルダを作る, フォルダに余分なファイルを足す)
    ("match-exact", True, True, False),
    ("match-extra-file", True, True, True),
    ("archive-only", True, False, False),
    ("folder-only", False, True, False),
]


def write_zip(path: Path, files: dict[str, bytes]) -> None:
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as archive:
        for name, data in files.items():
            archive.writestr(name, data)


def write_folder(root: Path, files: dict[str, bytes]) -> None:
    for name, data in files.items():
        target = root / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)


def fake_image(seed: int, size: int = 2048) -> bytes:
    """実画像の代わりに、判別しやすい固定パターンのバイト列を作る。"""
    rng = random.Random(seed)
    return bytes(rng.getrandbits(8) for _ in range(size))


def build(root: Path) -> None:
    root.mkdir(parents=True, exist_ok=True)

    for index, (album, count) in enumerate(ALBUMS):
        files = {f"{album}/photo-{n:02d}.jpg": fake_image(index * 100 + n) for n in range(1, count + 1)}
        write_zip(root / f"{album}.zip", files)
        write_folder(root / album, files)

    for index, (name, make_zip, make_folder, extra) in enumerate(CASES):
        files = {f"{name}/data-{n:02d}.bin": fake_image(9000 + index * 10 + n, 512) for n in range(1, 4)}
        if make_zip:
            write_zip(root / f"{name}.zip", files)
        if make_folder:
            write_folder(root / name, files)
            if extra:
                (root / name / "notes.txt").write_text("sample\n", encoding="utf-8")

    print(f"created sample data in {root}")


def main() -> None:
    if len(sys.argv) != 2:
        print(__doc__)
        raise SystemExit(2)
    build(Path(sys.argv[1]).resolve())


if __name__ == "__main__":
    main()
