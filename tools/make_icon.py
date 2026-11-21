#!/usr/bin/env python3
"""archive-extract-cleaner のアプリアイコンを生成する。

コンセプト:
  - 左にアーカイブ（ZIP）の箱、右に解凍済みフォルダ
  - 中央に照合のチェックマーク
  - 「一致したものだけを残す」というアプリの役割を1枚で示す

配色は UI の CSS 変数に合わせる（accent #2458d3 / green #107347）。
"""

from __future__ import annotations

import math
import struct
import zlib
from pathlib import Path

SIZE = 1024
ACCENT = (36, 88, 211)
ACCENT_LIGHT = (75, 123, 236)
GREEN = (16, 115, 71)
WHITE = (255, 255, 255)
INK = (29, 35, 43)


def blend(dst, src, alpha):
    """src を alpha で dst に重ねる。"""
    return tuple(round(d + (s - d) * alpha) for d, s in zip(dst, src))


class Canvas:
    def __init__(self, size: int, background: tuple[int, int, int]):
        self.size = size
        self.pixels = [list(background) for _ in range(size * size)]

    def set(self, x: int, y: int, color, alpha: float = 1.0):
        if not (0 <= x < self.size and 0 <= y < self.size) or alpha <= 0:
            return
        index = y * self.size + x
        self.pixels[index] = list(blend(tuple(self.pixels[index]), color, min(1.0, alpha)))

    def rounded_rect(self, x0, y0, x1, y1, radius, color, alpha=1.0):
        """角丸矩形を塗る。アンチエイリアスは縁の1pxで行う。"""
        for y in range(max(0, int(y0) - 1), min(self.size, int(y1) + 2)):
            for x in range(max(0, int(x0) - 1), min(self.size, int(x1) + 2)):
                coverage = self._rounded_coverage(x, y, x0, y0, x1, y1, radius)
                if coverage > 0:
                    self.set(x, y, color, alpha * coverage)

    @staticmethod
    def _rounded_coverage(x, y, x0, y0, x1, y1, radius) -> float:
        """点 (x+0.5, y+0.5) が角丸矩形の内側かを 0..1 で返す。"""
        px, py = x + 0.5, y + 0.5
        if px < x0 - 1 or px > x1 + 1 or py < y0 - 1 or py > y1 + 1:
            return 0.0
        # 中心からの距離で角の丸みを判定する。
        cx = min(max(px, x0 + radius), x1 - radius)
        cy = min(max(py, y0 + radius), y1 - radius)
        distance = math.hypot(px - cx, py - cy)
        if distance <= radius - 0.5:
            return 1.0
        if distance >= radius + 0.5:
            return 0.0
        return radius + 0.5 - distance

    def circle(self, cx, cy, radius, color, alpha=1.0):
        for y in range(max(0, int(cy - radius) - 1), min(self.size, int(cy + radius) + 2)):
            for x in range(max(0, int(cx - radius) - 1), min(self.size, int(cx + radius) + 2)):
                distance = math.hypot(x + 0.5 - cx, y + 0.5 - cy)
                if distance <= radius - 0.5:
                    self.set(x, y, color, alpha)
                elif distance < radius + 0.5:
                    self.set(x, y, color, alpha * (radius + 0.5 - distance))

    def thick_line(self, x0, y0, x1, y1, width, color, alpha=1.0):
        """太い線分を描く。チェックマーク用。"""
        half = width / 2
        min_x = int(min(x0, x1) - half - 1)
        max_x = int(max(x0, x1) + half + 2)
        min_y = int(min(y0, y1) - half - 1)
        max_y = int(max(y0, y1) + half + 2)
        dx, dy = x1 - x0, y1 - y0
        length_sq = dx * dx + dy * dy
        for y in range(max(0, min_y), min(self.size, max_y)):
            for x in range(max(0, min_x), min(self.size, max_x)):
                px, py = x + 0.5, y + 0.5
                if length_sq == 0:
                    distance = math.hypot(px - x0, py - y0)
                else:
                    t = max(0.0, min(1.0, ((px - x0) * dx + (py - y0) * dy) / length_sq))
                    distance = math.hypot(px - (x0 + t * dx), py - (y0 + t * dy))
                if distance <= half - 0.5:
                    self.set(x, y, color, alpha)
                elif distance < half + 0.5:
                    self.set(x, y, color, alpha * (half + 0.5 - distance))

    def to_png(self) -> bytes:
        raw = bytearray()
        for y in range(self.size):
            raw.append(0)  # filter type 0
            row = self.pixels[y * self.size : (y + 1) * self.size]
            for pixel in row:
                raw.extend(pixel)
        compressed = zlib.compress(bytes(raw), 9)

        def chunk(tag: bytes, data: bytes) -> bytes:
            return (
                struct.pack(">I", len(data))
                + tag
                + data
                + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)
            )

        header = struct.pack(">IIBBBBB", self.size, self.size, 8, 2, 0, 0, 0)
        return (
            b"\x89PNG\r\n\x1a\n"
            + chunk(b"IHDR", header)
            + chunk(b"IDAT", compressed)
            + chunk(b"IEND", b"")
        )


def build_icon() -> Canvas:
    canvas = Canvas(SIZE, WHITE)

    # 背景: 角丸の青いパネル。Windows のアイコンは角丸が自然。
    margin = 40
    canvas.rounded_rect(margin, margin, SIZE - margin, SIZE - margin, 190, ACCENT)
    # 上から下へ少し明るくなるグラデーション。
    for y in range(SIZE):
        t = y / SIZE
        color = tuple(round(a + (b - a) * t) for a, b in zip(ACCENT, ACCENT_LIGHT))
        for x in range(SIZE):
            if canvas.pixels[y * SIZE + x] != list(ACCENT):
                continue
            canvas.set(x, y, color, 0.55)

    # 左: アーカイブ（ZIP）の箱。白い角丸矩形にファスナーを描く。
    box_x0, box_y0, box_x1, box_y1 = 150, 300, 470, 730
    canvas.rounded_rect(box_x0, box_y0, box_x1, box_y1, 34, WHITE)
    # ファスナー（中央の縦線）と歯。歯は箱の内側に収める。
    zipper_x = (box_x0 + box_x1) // 2
    canvas.rounded_rect(zipper_x - 12, box_y0 + 46, zipper_x + 12, box_y1 - 46, 8, ACCENT)
    # 歯は箱の内側に均等に並べる。上下の余白から逆算してはみ出さないようにする。
    teeth_top = box_y0 + 90
    teeth_bottom = box_y1 - 90 - 26
    teeth_count = 5
    step = (teeth_bottom - teeth_top) // (teeth_count - 1)
    for i in range(teeth_count):
        y = teeth_top + i * step
        canvas.rounded_rect(zipper_x - 44, y, zipper_x - 20, y + 26, 7, ACCENT)
        canvas.rounded_rect(zipper_x + 20, y, zipper_x + 44, y + 26, 7, ACCENT)

    # 右: 解凍済みフォルダ。タブを本体の上辺に重ねて1つの形にする。
    folder_x0, folder_x1 = 560, 880
    body_y0, body_y1 = 400, 700
    # タブは本体の上辺から立ち上げる。本体と同じ白で塗る。
    canvas.rounded_rect(folder_x0, 330, folder_x0 + 200, body_y0 + 40, 26, WHITE)
    canvas.rounded_rect(folder_x0, body_y0, folder_x1, body_y1, 34, WHITE)

    # 中央: 照合のチェックマーク。緑の円に白いチェック。
    cx, cy, radius = 512, 512, 128
    canvas.circle(cx, cy, radius + 16, WHITE)
    canvas.circle(cx, cy, radius, GREEN)
    canvas.thick_line(cx - 58, cy + 2, cx - 14, cy + 48, 32, WHITE)
    canvas.thick_line(cx - 14, cy + 48, cx + 64, cy - 44, 32, WHITE)

    return canvas


def main() -> None:
    root = Path(__file__).resolve().parent.parent
    canvas = build_icon()
    png = canvas.to_png()
    target = root / "build" / "appicon.png"
    target.write_bytes(png)
    print(f"wrote {target} ({len(png)} bytes)")

    # Windows 用の .ico も作る。Wails は build/windows/icon.ico を参照する。
    # 複数サイズを入れておくと、タスクバー・エクスプローラー・小さい表示の
    # いずれでも綺麗に出る。
    try:
        from PIL import Image
    except ImportError:
        print("Pillow が無いため .ico は生成しません（pip install pillow）")
        return
    import io

    image = Image.open(io.BytesIO(png)).convert("RGBA")
    ico_path = root / "build" / "windows" / "icon.ico"
    ico_path.parent.mkdir(parents=True, exist_ok=True)
    sizes = [(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)]
    image.save(ico_path, format="ICO", sizes=sizes)
    print(f"wrote {ico_path} ({ico_path.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
