"""Shared procedural art toolkit for Reasonix official theme backgrounds.

All artwork is generated from scratch with numpy + PIL. No reference pixels,
no third-party assets, no text, no UI mockery. Fixed seeds make every render
reproducible; the SHA-256 of each output is recorded in PROVENANCE.
"""
from __future__ import annotations

import math
import os
import random

import numpy as np
from PIL import Image, ImageDraw, ImageFilter

W, H = 2560, 1440

# Layout contract (fractions of W/H) from the theme plan:
#   low-info zone  : x 0% - 52%
#   visual centre  : x 68% - 76%
#   key content box: x 62% - 88%, y 16% - 72%
KEY_X0, KEY_X1 = 0.62 * W, 0.88 * W
KEY_Y0, KEY_Y1 = 0.16 * H, 0.72 * H
FOCUS_X = 0.72 * W


def hex2rgb(s: str) -> tuple[int, int, int]:
    s = s.lstrip("#")
    return int(s[0:2], 16), int(s[2:4], 16), int(s[4:6], 16)


def mix(c1, c2, t: float):
    a, b = hex2rgb(c1) if isinstance(c1, str) else c1, hex2rgb(c2) if isinstance(c2, str) else c2
    return tuple(int(round(a[i] + (b[i] - a[i]) * t)) for i in range(3))


def rgba(c, a: int):
    return (c[0], c[1], c[2], max(0, min(255, int(a))))


def _stops_arrays(stops):
    pos = np.array([p for p, _ in stops], dtype=np.float64)
    cols = np.array([hex2rgb(c) for _, c in stops], dtype=np.float64)
    return pos, cols


def _interp_channel(pos, cols, t):
    out = np.zeros((*t.shape, 3), dtype=np.float64)
    for ch in range(3):
        out[..., ch] = np.interp(t, pos, cols[:, ch])
    return out


def gradient(w: int, h: int, stops, direction: str = "v") -> Image.Image:
    """Multi-stop gradient. direction: v | h | d1 (tl->br) | d2 (bl->tr) | r (radial from stops centre)."""
    pos, cols = _stops_arrays(stops)
    if direction == "v":
        t = np.linspace(0.0, 1.0, h)[:, None] * np.ones((1, w))
    elif direction == "h":
        t = np.ones((h, 1)) * np.linspace(0.0, 1.0, w)[None, :]
    elif direction == "d1":
        t = (np.linspace(0.0, 1.0, h)[:, None] + np.linspace(0.0, 1.0, w)[None, :]) / 2.0
    elif direction == "d2":
        t = (np.linspace(1.0, 0.0, h)[:, None] + np.linspace(0.0, 1.0, w)[None, :]) / 2.0
    else:
        raise ValueError(direction)
    arr = _interp_channel(pos, cols, t).astype(np.uint8)
    return Image.fromarray(arr, "RGB").convert("RGBA")


def new_layer() -> Image.Image:
    return Image.new("RGBA", (W, H), (0, 0, 0, 0))


def comp(base: Image.Image, layer: Image.Image, blur: float = 0.0) -> Image.Image:
    if blur > 0:
        layer = layer.filter(ImageFilter.GaussianBlur(blur))
    base.alpha_composite(layer)
    return base


def glow(base, cx, cy, r, color, alpha, squash=1.0):
    """Soft radial light blob (alpha peaks at centre)."""
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    rx, ry = r, r * squash
    steps = 28
    for i in range(steps, 0, -1):
        t = i / steps
        a = alpha * (1.0 - t) ** 1.6
        d.ellipse([cx - rx * t, cy - ry * t, cx + rx * t, cy + ry * t], fill=rgba(color, a))
    base.alpha_composite(lay.filter(ImageFilter.GaussianBlur(r * 0.10)))


def beam(base, apex, target, width0, width1, color, alpha, blur=24):
    """Spotlight cone from apex towards target point."""
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    ax, ay = apex
    tx, ty = target
    dx, dy = tx - ax, ty - ay
    ln = math.hypot(dx, dy) or 1.0
    nx, ny = -dy / ln, dx / ln
    pts = [
        (ax + nx * width0 / 2, ay + ny * width0 / 2),
        (tx + nx * width1 / 2, ty + ny * width1 / 2),
        (tx - nx * width1 / 2, ty - ny * width1 / 2),
        (ax - nx * width0 / 2, ay - ny * width0 / 2),
    ]
    d.polygon(pts, fill=rgba(color, alpha))
    base.alpha_composite(lay.filter(ImageFilter.GaussianBlur(blur)))


def cubic(p0, p1, p2, p3, n=48):
    pts = []
    for i in range(n + 1):
        t = i / n
        mt = 1 - t
        x = mt**3 * p0[0] + 3 * mt**2 * t * p1[0] + 3 * mt * t**2 * p2[0] + t**3 * p3[0]
        y = mt**3 * p0[1] + 3 * mt**2 * t * p1[1] + 3 * mt * t**2 * p2[1] + t**3 * p3[1]
        pts.append((x, y))
    return pts


def smooth_path(segments):
    """segments: list of (p0,p1,p2,p3) cubic tuples -> concatenated point list."""
    pts = []
    for seg in segments:
        part = cubic(*seg)
        if pts:
            part = part[1:]
        pts.extend(part)
    return pts


def ellipse_poly(cx, cy, rx, ry, n=72, a0=0.0, a1=2 * math.pi, rot=0.0):
    pts = []
    for i in range(n + 1):
        t = a0 + (a1 - a0) * i / n
        x, y = rx * math.cos(t), ry * math.sin(t)
        xr = x * math.cos(rot) - y * math.sin(rot)
        yr = x * math.sin(rot) + y * math.cos(rot)
        pts.append((cx + xr, cy + yr))
    return pts


def superellipse_poly(cx, cy, rx, ry, power=4.0, n=96, rot=0.0):
    """Rounded-rect-like closed curve; power 2 = ellipse, higher = boxier."""
    pts = []
    e = 2.0 / power
    for i in range(n):
        t = 2 * math.pi * i / n
        ct, st = math.cos(t), math.sin(t)
        x = rx * math.copysign(abs(ct) ** e, ct)
        y = ry * math.copysign(abs(st) ** e, st)
        xr = x * math.cos(rot) - y * math.sin(rot)
        yr = x * math.sin(rot) + y * math.cos(rot)
        pts.append((cx + xr, cy + yr))
    return pts


def star4(draw, cx, cy, r, color, alpha, thin=0.18, rot=0.0):
    """Four-point sparkle."""
    pts = []
    for i in range(8):
        ang = rot + math.pi / 4 * i
        rr = r if i % 2 == 0 else r * thin
        pts.append((cx + rr * math.cos(ang), cy + rr * math.sin(ang)))
    draw.polygon(pts, fill=rgba(color, alpha))


def add_grain(img: Image.Image, amount=3.0, seed=7):
    rng = np.random.default_rng(seed)
    noise = rng.normal(0.0, amount, (H, W, 1)).repeat(3, axis=2)
    arr = np.asarray(img.convert("RGB")).astype(np.int16) + noise.astype(np.int16)
    arr = np.clip(arr, 0, 255).astype(np.uint8)
    out = Image.fromarray(arr, "RGB").convert("RGBA")
    out.putalpha(img.split()[3] if img.mode == "RGBA" else 255)
    return out


def paper_texture(img, color="#000000", alpha=6, seed=3, scale=3):
    """Fine fibrous speckle for paper-like fields."""
    rng = np.random.default_rng(seed)
    small = rng.normal(0.0, 1.0, (H // scale, W // scale))
    t = Image.fromarray(((small - small.min()) / (small.ptp() + 1e-9) * 255).astype(np.uint8))
    t = t.resize((W, H), Image.BILINEAR).filter(ImageFilter.GaussianBlur(0.6))
    lay = Image.merge("RGBA", (t, t, t, t.point(lambda v: int(v / 255 * alpha))))
    tint = Image.new("RGBA", (W, H), rgba(hex2rgb(color), 255))
    lay = Image.composite(tint, new_layer(), lay.split()[3])
    img.alpha_composite(lay)


def petal_pts(cx, cy, size, angle):
    """A single rose petal outline (teardrop with curled tip)."""
    ca, sa = math.cos(angle), math.sin(angle)

    def tr(p):
        x, y = p
        return (cx + x * ca - y * sa, cy + x * sa + y * ca)

    segs = [
        ((0, 0), (0.55 * size, -0.42 * size), (1.05 * size, -0.28 * size), (1.18 * size, 0.10 * size)),
        ((1.18 * size, 0.10 * size), (1.26 * size, 0.42 * size), (0.72 * size, 0.62 * size), (0.28 * size, 0.55 * size)),
        ((0.28 * size, 0.55 * size), (-0.05 * size, 0.50 * size), (-0.10 * size, 0.18 * size), (0, 0)),
    ]
    return [tr(p) for p in smooth_path(segs)]


def leaf_pts(cx, cy, length, width, angle, curl=0.35):
    ca, sa = math.cos(angle), math.sin(angle)

    def tr(p):
        x, y = p
        return (cx + x * ca - y * sa, cy + x * sa + y * ca)

    segs = [
        ((0, 0), (0.30 * length, -width), (0.75 * length, -width * 0.9), (length, -curl * width)),
        ((length, -curl * width), (0.72 * length, width * 0.7), (0.32 * length, width), (0, 0)),
    ]
    return [tr(p) for p in smooth_path(segs)]


def butterfly_pts(cx, cy, size, angle, flap=1.0):
    """Stylised butterfly: two upper + two lower wings + body, returns list of polys."""
    ca, sa = math.cos(angle), math.sin(angle)

    def tr(p):
        x, y = p
        return (cx + x * ca - y * sa, cy + x * sa + y * ca)

    polys = []
    for sgn in (-1, 1):
        upper = smooth_path([
            ((0, 0), (sgn * 0.95 * size, -0.85 * size * flap), (sgn * 1.45 * size, -0.55 * size * flap), (sgn * 1.30 * size, -0.02 * size)),
            ((sgn * 1.30 * size, -0.02 * size), (sgn * 1.05 * size, 0.28 * size), (sgn * 0.35 * size, 0.22 * size), (0, 0.10 * size)),
        ])
        polys.append([tr(p) for p in upper])
        lower = smooth_path([
            ((0, 0.08 * size), (sgn * 0.72 * size, 0.28 * size), (sgn * 0.88 * size, 0.78 * size), (sgn * 0.42 * size, 1.02 * size)),
            ((sgn * 0.42 * size, 1.02 * size), (sgn * 0.10 * size, 0.95 * size), (sgn * 0.02 * size, 0.42 * size), (0, 0.22 * size)),
        ])
        polys.append([tr(p) for p in lower])
    body = ellipse_poly(cx, cy, 0.09 * size, 0.42 * size, rot=angle)
    return polys, body


def cloud_curl_pts(cx, cy, size, color_flip=False):
    """Auspicious-cloud (spiral scroll) outline, flat motif."""
    pts = []
    turns = 1.65
    for i in range(90):
        t = i / 89
        ang = turns * 2 * math.pi * t + math.pi * 0.5
        r = size * (1.0 - 0.72 * t)
        pts.append((cx + r * math.cos(ang), cy + 0.62 * r * math.sin(ang)))
    # outer tail sweeping right
    tail = smooth_path([
        (pts[0], (cx + 1.9 * size, cy - 0.9 * size), (cx + 2.9 * size, cy - 0.4 * size), (cx + 3.3 * size, cy + 0.35 * size)),
    ])
    return pts, tail


def coin_pts(cx, cy, r, rot=0.0):
    """Round coin with rounded-square hole (abstract lucky coin, no characters)."""
    outer = ellipse_poly(cx, cy, r, r, rot=rot)
    hole = superellipse_poly(cx, cy, r * 0.34, r * 0.34, power=4.5, rot=rot)
    return outer, hole


def ring_pts(cx, cy, r, width, a0=0.0, a1=2 * math.pi, squash=1.0):
    outer = ellipse_poly(cx, cy, r, r * squash, a0=a0, a1=a1)
    inner = ellipse_poly(cx, cy, r - width, (r - width) * squash, a0=a1, a1=a0)
    return outer + inner


def draw_poly(draw, pts, color, alpha=255, outline=None, outline_w=0):
    draw.polygon(pts, fill=rgba(color, alpha))
    if outline and outline_w > 0:
        draw.line(pts + [pts[0]], fill=outline, width=outline_w, joint="curve")


def soft_fill(base, pts, color, alpha, blur=0.0):
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    d.polygon(pts, fill=rgba(color, alpha))
    comp(base, lay, blur)


def save_webp(img: Image.Image, path: str, quality=82, target_bytes=None):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    rgb = img.convert("RGB")
    q = quality
    while True:
        rgb.save(path, "WEBP", quality=q, method=6, exact=True)
        size = os.path.getsize(path)
        if target_bytes is None or size <= target_bytes or q <= 40:
            return size
        q -= 6


def make_thumb(src: Image.Image, path: str, quality=76, target_bytes=120 * 1024):
    thumb = src.convert("RGB").resize((480, 270), Image.LANCZOS)
    q = quality
    while True:
        thumb.save(path, "WEBP", quality=q, method=6, exact=True)
        size = os.path.getsize(path)
        if size <= target_bytes or q <= 30:
            return size
        q -= 8


def sha256_file(path: str) -> str:
    import hashlib

    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def rng(seed: int) -> random.Random:
    return random.Random(seed)
