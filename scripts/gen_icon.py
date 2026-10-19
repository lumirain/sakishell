#!/usr/bin/env python3
"""SakiShell 应用图标生成器（纯 Python 标准库，无第三方依赖）。

在构建前运行：python3 scripts/gen_icon.py
输出：
  build/appicon.png      512x512 应用图标（Wails 使用）
  build/windows/icon.ico Windows 多尺寸图标（NSIS 安装包使用）

图标设计：深青蓝圆角背景 + 白色终端窗口（红/橙/绿窗口点、> 提示符、光标）+ 底部 MCP 连接点。
"""
import os
import struct
import tempfile
import zlib

SIZE = 512
ROUND = 104          # 背景圆角半径
BG_TOP = (15, 23, 42)       # #0f172a
BG_MID = (22, 78, 99)       # #164e63
BG_BOT = (8, 145, 178)      # #0891b2
WHITE = (255, 255, 255)
TERM_BOT = (226, 244, 255)  # #e2f4ff
DARK = (15, 23, 42)
CYAN = (8, 145, 178)
DOTS = [(132, 160, (239, 68, 68)),   # 红
        (168, 160, (245, 158, 11)),  # 橙
        (204, 160, (34, 197, 94))]   # 绿


def clamp(v, lo=0, hi=255):
    return max(lo, min(hi, v))


def lerp_color(a, b, t):
    return tuple(int(a[i] + (b[i] - a[i]) * t) for i in range(3))


def in_round_rect(x, y, x0, y0, x1, y1, r):
    if x < x0 or x > x1 or y < y0 or y > y1:
        return False
    if x0 + r <= x <= x1 - r or y0 + r <= y <= y1 - r:
        return True
    cx = x0 + r if x < x0 + r else x1 - r
    cy = y0 + r if y < y0 + r else y1 - r
    return (x - cx) ** 2 + (y - cy) ** 2 <= r * r


def in_circle(x, y, cx, cy, r):
    return (x - cx) ** 2 + (y - cy) ** 2 <= r * r


def draw():
    px = bytearray(SIZE * SIZE * 4)
    for y in range(SIZE):
        t = y / (SIZE - 1)
        if t < 0.55:
            bg = lerp_color(BG_TOP, BG_MID, t / 0.55)
        else:
            bg = lerp_color(BG_MID, BG_BOT, (t - 0.55) / 0.45)
        for x in range(SIZE):
            idx = (y * SIZE + x) * 4
            if not in_round_rect(x, y, 16, 16, 495, 495, ROUND):
                continue
            color = bg
            # 终端窗口（96,120)-(416,392) 圆角 40
            if in_round_rect(x, y, 96, 120, 416, 392, 40):
                tt = (y - 120) / 272
                color = lerp_color(WHITE, TERM_BOT, tt)
                # 窗口标题栏圆点
                for cx, cy, c in DOTS:
                    if in_circle(x, y, cx, cy, 12):
                        color = c
                # > 提示符（粗线三角带）
                if 132 <= x <= 168 and 250 <= y <= 302:
                    if (x - 132) / 36 < 1 and (y - 250) / 52 < 1 and (x - 132) / 36 + (y - 250) / 52 >= 0.72:
                        color = DARK
                # 提示符横条
                if 196 <= x <= 336 and 248 <= y <= 274 and not (x <= 168 and y >= 250):
                    color = DARK
                # 光标条
                if 196 <= x <= 214 and 296 <= y <= 340:
                    color = CYAN
            # 底部 MCP 连接点
            if y >= 408:
                for cx in (160, 256, 352):
                    if in_circle(x, y, cx, 424, 16):
                        color = WHITE
                if (176 <= x <= 240 or 272 <= x <= 336) and 419 <= y <= 429:
                    color = lerp_color(WHITE, bg, 0.4)
            px[idx:idx + 3] = bytes(color)
            px[idx + 3] = 255
    return px


def write_png(path, px, size):
    def chunk(tag, data):
        c = struct.pack('>I', len(data)) + tag + data
        return c + struct.pack('>I', zlib.crc32(tag + data) & 0xffffffff)
    raw = bytearray()
    stride = size * 4
    for y in range(size):
        raw += b'\x00' + px[y * stride:(y + 1) * stride]
    ihdr = struct.pack('>IIBBBBB', size, size, 8, 6, 0, 0, 0)
    png = b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', ihdr) + chunk(b'IDAT', zlib.compress(bytes(raw), 9)) + chunk(b'IEND', b'')
    with open(path, 'wb') as f:
        f.write(png)


def downsample(px, src=512, dst=256):
    """2x2 平均降采样（含 alpha）。"""
    out = bytearray(dst * dst * 4)
    for y in range(dst):
        for x in range(dst):
            s = [0.0] * 4
            for dy in range(2):
                for dx in range(2):
                    idx = ((y * 2 + dy) * src + (x * 2 + dx)) * 4
                    for c in range(4):
                        s[c] += px[idx + c]
            oi = (y * dst + x) * 4
            for c in range(4):
                out[oi + c] = clamp(int(s[c] / 4))
    return out


def main():
    root = os.path.dirname(os.path.abspath(__file__))
    build = os.path.join(root, '..', 'build')
    win = os.path.join(build, 'windows')
    os.makedirs(win, exist_ok=True)
    px = draw()
    png_path = os.path.join(build, 'appicon.png')
    write_png(png_path, px, SIZE)
    # ICO 使用 256x256 降采样 PNG（Windows 标准最大 256）
    ico_px = downsample(px, SIZE, 256)
    ico_path = os.path.join(win, 'icon.ico')
    tmp_png = os.path.join(tempfile.gettempdir(), 'saki_ico_256.png')
    write_png(tmp_png, ico_px, 256)
    with open(tmp_png, 'rb') as f:
        png256 = f.read()
    try:
        os.remove(tmp_png)
    except OSError:
        pass
    with open(ico_path, 'wb') as f:
        f.write(b'\x00\x00\x01\x00\x01\x00' + struct.pack('<BBBBHHII', 0, 0, 0, 0, 1, 32, len(png256), 22) + png256)
    print('generated:', png_path, os.path.getsize(png_path), 'bytes')
    print('generated:', ico_path, os.path.getsize(ico_path), 'bytes')


if __name__ == '__main__':
    main()
