"""Per-theme scene generators. Each returns a 2560x1440 RGBA image.

Layout contract (from the theme plan):
  - left 0-52% of the width stays a low-information zone
  - main visual centre sits at x 68-76%
  - key content stays inside x 62-88%, y 16-72%
  - no windows/sidebars/cards/buttons/inputs, no readable text, no logos
"""
from __future__ import annotations

import math

from PIL import Image, ImageDraw, ImageFilter

from artkit import (
    H,
    W,
    beam,
    butterfly_pts,
    cloud_curl_pts,
    coin_pts,
    comp,
    cubic,
    draw_poly,
    ellipse_poly,
    glow,
    gradient,
    hex2rgb,
    leaf_pts,
    mix,
    new_layer,
    petal_pts,
    rgba,
    ring_pts,
    rng,
    smooth_path,
    star4,
    superellipse_poly,
)
from figures import (
    anime_adult,
    man_bust,
    mascot_programmer,
    performer,
    silhouette_muse,
    woman_serene,
)


def _rose(d, cx, cy, r, deep, mid, light, seed=0):
    """Layered flat rose head."""
    rr = rng(seed)
    for ring_i, (frac, col) in enumerate([(1.0, mid), (0.72, light), (0.46, mid), (0.26, deep)]):
        n = 7 if ring_i < 2 else 5
        for i in range(n):
            ang = (2 * math.pi * i / n) + ring_i * 0.45 + rr.uniform(-0.06, 0.06)
            px = cx + math.cos(ang) * r * frac * 0.34
            py = cy + math.sin(ang) * r * frac * 0.34
            d.polygon(petal_pts(px, py, r * frac * 0.72, ang + math.pi / 2), fill=rgba(col, 255))
    # centre spiral
    pts = []
    for i in range(40):
        t = i / 39
        a = 4.6 * math.pi * t
        pts.append((cx + math.cos(a) * r * 0.20 * (1 - t * 0.75), cy + math.sin(a) * r * 0.20 * (1 - t * 0.75)))
    d.line(pts, fill=rgba(deep, 220), width=max(2, int(r * 0.05)), joint="curve")


def scene_rose_dawn() -> "Image":
    R = rng(20260117)
    img = gradient(W, H, [(0.0, "#FFF9F5"), (0.45, "#FDF0F1"), (0.8, "#F9E0E4"), (1.0, "#F5D3DA")], "d1")
    glow(img, 0.78 * W, 0.30 * H, 0.55 * W, hex2rgb("#FFFFFF"), 70)
    glow(img, 0.86 * W, 0.72 * H, 0.38 * W, hex2rgb("#F2B7C4"), 60)

    # faint drifting petals on the low-info left
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    for _ in range(9):
        x = R.uniform(0.04, 0.48) * W
        y = R.uniform(0.10, 0.92) * H
        s = R.uniform(26, 52)
        d.polygon(petal_pts(x, y, s, R.uniform(0, 6.28)), fill=rgba(hex2rgb("#EFC4CD"), R.randint(28, 55)))
    comp(img, lay, 2)

    # woman
    woman_serene(
        img, 0.722 * W, 0.40 * H, 152,
        skin=hex2rgb("#F6D8C6"), hair_c=hex2rgb("#5E3A36"),
        cloth_c=hex2rgb("#FCEFEA"), lip_c=hex2rgb("#C45B73"), blush_c=hex2rgb("#EBA9B5"),
        style="long",
    )

    # rose cluster bottom-right + one at shoulder height
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    roses = [
        (0.865 * W, 0.82 * H, 150, 1),
        (0.940 * W, 0.62 * H, 108, 2),
        (0.780 * W, 0.93 * H, 124, 3),
        (0.925 * W, 0.95 * H, 84, 4),
        (0.620 * W, 0.90 * H, 56, 5),
    ]
    deep, mid, light = hex2rgb("#B43F65"), hex2rgb("#E28CA1"), hex2rgb("#F5C3CE")
    leaf_c = hex2rgb("#8A9A7B")
    for cx, cy, r, seed in roses:
        for i in range(3):
            ang = R.uniform(0, 6.28)
            d.polygon(leaf_pts(cx + math.cos(ang) * r * 0.9, cy + math.sin(ang) * r * 0.9, r * 0.85, r * 0.30, ang), fill=rgba(leaf_c, 235))
        _rose(d, cx, cy, r, deep, mid, light, seed=seed)
    # stems curling from cluster
    stem = cubic((0.86 * W, 0.98 * H), (0.83 * W, 0.90 * H), (0.84 * W, 0.84 * H), (0.855 * W, 0.80 * H), n=30)
    d.line(stem, fill=rgba(mix(leaf_c, "#000000", 0.15), 200), width=10, joint="curve")
    comp(img, lay, 0.8)

    # loose petals around the figure (right half only)
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    for _ in range(14):
        x = R.uniform(0.58, 0.97) * W
        y = R.uniform(0.08, 0.85) * H
        s = R.uniform(20, 46)
        col = mid if R.random() < 0.5 else light
        d.polygon(petal_pts(x, y, s, R.uniform(0, 6.28)), fill=rgba(col, R.randint(120, 200)))
    for _ in range(10):
        x, y = R.uniform(0.60, 0.98) * W, R.uniform(0.10, 0.80) * H
        d.ellipse([x - 5, y - 5, x + 5, y + 5], fill=rgba(hex2rgb("#FFFFFF"), R.randint(90, 160)))
    comp(img, lay, 1.2)

    # soft light rays upper-right
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    for i, ang in enumerate([-0.5, -0.28, -0.06]):
        x0, y0 = 0.99 * W, -0.05 * H
        x1 = 0.60 * W + i * 0.09 * W
        d.polygon([(x0, y0), (x0 - 0.10 * W, y0), (x1 - 0.06 * W, 0.75 * H), (x1 + 0.10 * W, 0.75 * H)], fill=rgba(hex2rgb("#FFFFFF"), 26))
    comp(img, lay, 30)
    return img


def _ingot(d, cx, cy, w, gold, deep):
    """Abstract gold ingot (no characters)."""
    body = smooth_path([
        ((cx - w * 0.5, cy + w * 0.10), (cx - w * 0.62, cy - w * 0.28), (cx - w * 0.30, cy - w * 0.30), (cx - w * 0.16, cy - w * 0.12)),
        ((cx - w * 0.16, cy - w * 0.12), (cx - w * 0.08, cy - w * 0.02), (cx + w * 0.08, cy - w * 0.02), (cx + w * 0.16, cy - w * 0.12)),
        ((cx + w * 0.16, cy - w * 0.12), (cx + w * 0.30, cy - w * 0.30), (cx + w * 0.62, cy - w * 0.28), (cx + w * 0.5, cy + w * 0.10)),
        ((cx + w * 0.5, cy + w * 0.10), (cx + w * 0.3, cy + w * 0.30), (cx - w * 0.3, cy + w * 0.30), (cx - w * 0.5, cy + w * 0.10)),
    ])
    d.polygon(body, fill=rgba(gold, 255))
    d.ellipse([cx - w * 0.16, cy - w * 0.16, cx + w * 0.16, cy + w * 0.06], fill=rgba(mix(gold, "#ffffff", 0.35), 255))
    d.line([(cx - w * 0.30, cy + w * 0.16), (cx + w * 0.30, cy + w * 0.16)], fill=rgba(deep, 130), width=max(2, int(w * 0.03)))


def scene_fortune_forge() -> "Image":
    R = rng(20260118)
    img = gradient(W, H, [(0.0, "#FFFBEF"), (0.5, "#FDF2D8"), (0.85, "#F8E3BC"), (1.0, "#F3D6A2")], "d1")
    glow(img, 0.76 * W, 0.34 * H, 0.5 * W, hex2rgb("#FFF6DC"), 80)
    glow(img, 0.88 * W, 0.80 * H, 0.36 * W, hex2rgb("#F0B64E"), 70)
    glow(img, 0.70 * W, 0.18 * H, 0.30 * W, hex2rgb("#E8604C"), 26)

    # faint auspicious clouds far left (low info)
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    for cx, cy, s in [(0.10 * W, 0.78 * H, 60), (0.30 * W, 0.22 * H, 46), (0.44 * W, 0.60 * H, 40)]:
        pts, tail = cloud_curl_pts(cx, cy, s)
        d.line(pts, fill=rgba(hex2rgb("#E4C493"), 70), width=9, joint="curve")
        d.line(tail, fill=rgba(hex2rgb("#E4C493"), 60), width=9, joint="curve")
    comp(img, lay, 3)

    # lucky programmer mascot
    mascot_programmer(
        img, 0.718 * W, 0.41 * H, 152,
        skin=hex2rgb("#F3CFAE"), hair_c=hex2rgb("#3A2A22"),
        cloth_c=hex2rgb("#B23A2C"), accent_c=hex2rgb("#E8AD38"), glasses_c=hex2rgb("#4A3226"),
    )

    # coins, ingots, clouds, jade beads (right side)
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    gold, gold_deep = hex2rgb("#E8AD38"), hex2rgb("#B97F1E")
    verm = hex2rgb("#C8402F")
    jade = hex2rgb("#3E7C5F")
    coins = [
        (0.620 * W, 0.30 * H, 54, 0.3), (0.650 * W, 0.62 * H, 40, -0.4), (0.860 * W, 0.24 * H, 62, 0.2),
        (0.920 * W, 0.46 * H, 44, 0.5), (0.600 * W, 0.82 * H, 48, -0.2), (0.880 * W, 0.70 * H, 38, 0.1),
    ]
    for cx, cy, r, rot in coins:
        outer, hole = coin_pts(cx, cy, r, rot)
        d.polygon(outer, fill=rgba(gold, 255))
        d.polygon(hole, fill=rgba(mix(gold, "#7a5310", 0.45), 255))
        inner, inner_hole = coin_pts(cx, cy, r * 0.74, rot)
        d.line(inner, fill=rgba(gold_deep, 160), width=max(3, int(r * 0.06)))
        d.polygon(inner_hole, fill=rgba(gold, 255))
    _ingot(d, 0.845 * W, 0.90 * H, 300, gold, gold_deep)
    _ingot(d, 0.640 * W, 0.94 * H, 210, mix(gold, "#ffffff", 0.12), gold_deep)
    # auspicious clouds around mascot
    for cx, cy, s, col, alp in [
        (0.600 * W, 0.52 * H, 42, verm, 200), (0.905 * W, 0.58 * H, 60, verm, 190),
        (0.855 * W, 0.16 * H, 44, mix(verm, gold, 0.4), 170),
    ]:
        pts, tail = cloud_curl_pts(cx, cy, s)
        d.line(pts, fill=rgba(col, alp), width=11, joint="curve")
        d.line(tail, fill=rgba(col, int(alp * 0.85)), width=11, joint="curve")
    # jade beads strand
    for i in range(5):
        bx, by = 0.925 * W + math.sin(i * 0.9) * 26, 0.80 * H + i * 46
        d.ellipse([bx - 17, by - 17, bx + 17, by + 17], fill=rgba(mix(jade, "#ffffff", 0.08 * (i % 2)), 240))
        d.ellipse([bx - 6, by - 8, bx + 4, by + 2], fill=rgba(mix(jade, "#ffffff", 0.6), 160))
    # sparks
    for _ in range(16):
        x, y = R.uniform(0.58, 0.97) * W, R.uniform(0.10, 0.92) * H
        star4(d, x, y, R.uniform(8, 22), gold if R.random() < 0.6 else verm, R.randint(120, 210))
    comp(img, lay, 0.6)

    # warm confetti dots
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    for _ in range(26):
        x, y = R.uniform(0.56, 0.98) * W, R.uniform(0.08, 0.95) * H
        r = R.uniform(3, 8)
        col = [gold, verm, jade][R.randint(0, 2)]
        d.ellipse([x - r, y - r, x + r, y + r], fill=rgba(col, R.randint(60, 140)))
    comp(img, lay, 1.5)
    return img


def scene_crimson_horizon() -> "Image":
    """Pearl-white sky + coral-red future city. No people."""
    R = rng(20260119)
    img = gradient(W, H, [(0.0, "#FFFAF8"), (0.5, "#FBECE9"), (0.85, "#F5D8D3"), (1.0, "#F0C9C3")], "v")
    glow(img, 0.74 * W, 0.30 * H, 0.42 * W, hex2rgb("#FFFFFF"), 80)

    # sun disc with soft rings
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    sun_c = hex2rgb("#F2A3A0")
    d.ellipse([0.74 * W - 150, 0.28 * H - 150, 0.74 * W + 150, 0.28 * H + 150], fill=rgba(mix(sun_c, "#ffffff", 0.25), 255))
    for rr, aa in [(210, 70), (280, 45)]:
        d.ellipse([0.74 * W - rr, 0.28 * H - rr, 0.74 * W + rr, 0.28 * H + rr], outline=rgba(sun_c, aa), width=6)
    comp(img, lay, 1.5)
    glow(img, 0.74 * W, 0.28 * H, 320, sun_c, 46)

    # distant faint hills across the width (kept very light)
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    hills = smooth_path([
        ((0, 0.74 * H), (0.18 * W, 0.68 * H), (0.36 * W, 0.72 * H), (0.52 * W, 0.66 * H)),
        ((0.52 * W, 0.66 * H), (0.72 * W, 0.60 * H), (0.88 * W, 0.66 * H), (W, 0.62 * H)),
        ((W, 0.62 * H), (W, 0.80 * H), (0.5 * W, 0.82 * H), (0, 0.80 * H)),
    ])
    d.polygon(hills, fill=rgba(hex2rgb("#EFCFC9"), 110))
    comp(img, lay, 6)

    # future city skyline, right half only
    deep = hex2rgb("#B92B38")
    mid = hex2rgb("#D3606A")
    light = hex2rgb("#E89B9B")
    pale = hex2rgb("#F3C2BC")
    base_y = 0.985 * H

    lay = new_layer()
    d = ImageDraw.Draw(lay)

    def tower(x, w, h, col, style="flat"):
        pts = [(x, base_y), (x, base_y - h)]
        if style == "step":
            pts += [(x + w * 0.18, base_y - h), (x + w * 0.18, base_y - h * 1.12), (x + w * 0.82, base_y - h * 1.12), (x + w * 0.82, base_y - h)]
        elif style == "spire":
            pts += [(x + w * 0.30, base_y - h), (x + w * 0.5, base_y - h * 1.38), (x + w * 0.70, base_y - h)]
        elif style == "dome":
            arc = ellipse_poly(x + w / 2, base_y - h, w / 2, w * 0.62, a0=math.pi, a1=2 * math.pi)
            pts += arc
        elif style == "slant":
            pts += [(x + w * 0.85, base_y - h * 1.06)]
        pts += [(x + w, base_y - h if style != "slant" else base_y - h * 0.9), (x + w, base_y)]
        d.polygon(pts, fill=rgba(col, 255))
        return x + w

    # back row (pale)
    x = 0.535 * W
    for w, h, st in [(120, 300, "flat"), (90, 420, "slant"), (150, 260, "dome"), (80, 520, "spire"), (130, 340, "step"), (100, 240, "flat"), (140, 400, "slant"), (95, 300, "dome"), (120, 360, "step")]:
        x = tower(x, w * 1.5, h * 1.05, pale, st) + 18
    comp(img, lay, 1.2)

    # mid row (light/mid)
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    x = 0.56 * W
    for w, h, st, col in [(110, 430, "step", light), (85, 600, "spire", mid), (140, 380, "dome", light), (95, 700, "slant", mid), (120, 470, "step", light), (80, 560, "spire", mid), (150, 350, "flat", light), (105, 640, "slant", mid)]:
        x = tower(x, w * 1.5, h * 1.05, col, st) + 26
    comp(img, lay, 0.8)

    # front row (deep) with light band windows
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    x = 0.585 * W
    fronts = [(130, 640, "step"), (95, 820, "spire"), (160, 520, "dome"), (110, 900, "slant"), (135, 600, "step"), (100, 740, "spire"), (150, 480, "dome")]
    for w, h, st in fronts:
        x0 = x
        x = tower(x, w * 1.5, h * 1.05, deep, st) + 34
        for fy in range(int(base_y - h * 1.05 + 60), int(base_y - 60), 92):
            d.rectangle([x0 + 14, fy, x0 + w * 1.5 - 14, fy + 12], fill=rgba(hex2rgb("#F6D9D4"), 150))
    # monorail arc sweeping to the right edge
    rail = cubic((0.60 * W, 0.86 * H), (0.75 * W, 0.78 * H), (0.90 * W, 0.80 * H), (1.02 * W, 0.72 * H), n=60)
    d.line(rail, fill=rgba(mix(deep, "#000000", 0.1), 200), width=12, joint="curve")
    comp(img, lay, 0.6)

    # birds + floating particles
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    for bx, by, s in [(0.60 * W, 0.20 * H, 22), (0.64 * W, 0.16 * H, 16), (0.90 * W, 0.14 * H, 20), (0.86 * W, 0.22 * H, 14)]:
        d.line(cubic((bx - s, by), (bx - s * 0.4, by - s * 0.7), (bx + s * 0.4, by - s * 0.7), (bx + s, by), n=16), fill=rgba(deep, 170), width=5, joint="curve")
    for _ in range(14):
        x, y = R.uniform(0.56, 0.98) * W, R.uniform(0.08, 0.55) * H
        r = R.uniform(3, 7)
        d.ellipse([x - r, y - r, x + r, y + r], fill=rgba(mid, R.randint(50, 110)))
    comp(img, lay, 1.0)
    return img


def _sprig(d, x, y, length, angle, n, col, col2, leaf_w=0.30):
    """Sage sprig: curved stem with opposite oval leaves."""
    ca, sa = math.cos(angle), math.sin(angle)
    tip = (x + ca * length, y + sa * length)
    bend = (x + ca * length * 0.5 - sa * length * 0.12, y + sa * length * 0.5 + ca * length * 0.12)
    stem = cubic((x, y), bend, (bend[0] + ca * 10, bend[1] + sa * 10), tip, n=40)
    d.line(stem, fill=rgba(mix(col, "#000000", 0.12), 230), width=7, joint="curve")
    for i in range(n):
        t = 0.18 + 0.78 * i / max(1, n - 1)
        px = x + ca * length * t - sa * length * 0.10 * math.sin(t * math.pi)
        py = y + sa * length * t + ca * length * 0.10 * math.sin(t * math.pi)
        ll = length * (0.20 - 0.10 * t)
        for sgn in (-1, 1):
            la = angle + sgn * (1.15 - 0.4 * t)
            d.polygon(leaf_pts(px, py, ll, ll * leaf_w * 2.2, la, curl=0.1), fill=rgba(col if (i + sgn) % 2 == 0 else col2, 245))


def scene_sage_breeze() -> "Image":
    R = rng(20260120)
    img = gradient(W, H, [(0.0, "#FAFAF3"), (0.55, "#F2F2E6"), (1.0, "#E7E9D8")], "d1")
    from artkit import paper_texture

    paper_texture(img, "#8a946f", alpha=7, seed=11)
    glow(img, 0.76 * W, 0.30 * H, 0.5 * W, hex2rgb("#FFFFFF"), 60)
    glow(img, 0.88 * W, 0.85 * H, 0.35 * W, hex2rgb("#B9CBA8"), 55)

    # faint sprig silhouettes far left
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    sage, sage2 = hex2rgb("#47735F"), hex2rgb("#7A9B84")
    _sprig(d, 0.10 * W, 0.95 * H, 330, -1.25, 6, sage2, sage2)
    _sprig(d, 0.30 * W, 0.98 * H, 260, -1.05, 5, sage2, sage2)
    alpha = lay.split()[3].point(lambda v: int(v * 0.45))
    lay.putalpha(alpha)
    comp(img, lay, 1.5)

    # adult man
    man_bust(
        img, 0.722 * W, 0.40 * H, 150,
        skin=hex2rgb("#EDCBAA"), hair_c=hex2rgb("#3B352E"),
        cloth_c=hex2rgb("#57765F"), collar_c=hex2rgb("#EFF0E2"), style="shirt",
    )

    # open book in his hands (foreground)
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    bx, by, bw = 0.722 * W, 0.90 * H, 520
    cover_l = [(bx - bw / 2 - 12, by - 66), (bx, by - 56), (bx, by + 34), (bx - bw / 2 - 12, by + 24)]
    cover_r = [(bx + bw / 2 + 12, by - 66), (bx, by - 56), (bx, by + 34), (bx + bw / 2 + 12, by + 24)]
    d.polygon(cover_l, fill=rgba(mix(sage, "#000000", 0.18), 255))
    d.polygon(cover_r, fill=rgba(mix(sage, "#000000", 0.18), 255))
    page_l = [(bx - bw / 2, by - 78), (bx, by - 68), (bx, by + 22), (bx - bw / 2, by + 12)]
    page_r = [(bx + bw / 2, by - 78), (bx, by - 68), (bx, by + 22), (bx + bw / 2, by + 12)]
    d.polygon(page_l, fill=rgba(hex2rgb("#FCFBF2"), 255))
    d.polygon(page_r, fill=rgba(hex2rgb("#F2F0E0"), 255))
    d.line([(bx, by - 68), (bx, by + 22)], fill=rgba(mix(sage, "#000000", 0.05), 200), width=5)
    for i in range(3):
        t = i / 2
        yy0 = by - 60 + i * 22
        d.line([(bx - bw * 0.42, yy0 - 6 * (1 - t)), (bx - bw * 0.07, yy0 + 2)], fill=rgba(sage2, 110), width=6)
        d.line([(bx + bw * 0.07, yy0 + 2), (bx + bw * 0.42, yy0 - 6 * (1 - t))], fill=rgba(sage2, 110), width=6)
    comp(img, lay, 0.5)

    # sage sprigs foreground right + tall behind
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    _sprig(d, 0.60 * W, 0.99 * H, 380, -1.35, 7, sage, sage2)
    _sprig(d, 0.86 * W, 1.0 * H, 430, -1.15, 8, sage, sage2)
    _sprig(d, 0.94 * W, 0.99 * H, 340, -1.45, 6, sage2, sage)
    _sprig(d, 0.665 * W, 0.995 * H, 300, -0.9, 5, sage2, sage)
    comp(img, lay, 0.8)

    # drifting seeds / pollen
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    for _ in range(18):
        x, y = R.uniform(0.55, 0.98) * W, R.uniform(0.10, 0.85) * H
        r = R.uniform(2.5, 6)
        d.ellipse([x - r, y - r, x + r, y + r], fill=rgba(sage2, R.randint(60, 130)))
    for _ in range(6):
        x, y = R.uniform(0.05, 0.45) * W, R.uniform(0.15, 0.9) * H
        r = R.uniform(2, 5)
        d.ellipse([x - r, y - r, x + r, y + r], fill=rgba(sage2, R.randint(25, 50)))
    comp(img, lay, 1.5)
    return img


def _pencil(d, cx, cy, length, angle, body_c, wood_c, tip_c, eraser_c):
    ca, sa = math.cos(angle), math.sin(angle)

    def tr(p):
        x, y = p
        return (cx + x * ca - y * sa, cy + x * sa + y * ca)

    wdt = length * 0.075
    body = [tr(p) for p in [(-length / 2, -wdt / 2), (length * 0.32, -wdt / 2), (length * 0.32, wdt / 2), (-length / 2, wdt / 2)]]
    d.polygon(body, fill=rgba(body_c, 255))
    wood = [tr(p) for p in [(length * 0.32, -wdt / 2), (length * 0.44, 0), (length * 0.32, wdt / 2)]]
    d.polygon(wood, fill=rgba(wood_c, 255))
    tip = [tr(p) for p in [(length * 0.40, -wdt * 0.22), (length * 0.44, 0), (length * 0.40, wdt * 0.22)]]
    d.polygon(tip, fill=rgba(tip_c, 255))
    era = [tr(p) for p in [(-length / 2, -wdt / 2), (-length * 0.44, -wdt / 2), (-length * 0.44, wdt / 2), (-length / 2, wdt / 2)]]
    d.polygon(era, fill=rgba(eraser_c, 255))
    band = [tr(p) for p in [(-length * 0.44, -wdt / 2), (-length * 0.40, -wdt / 2), (-length * 0.40, wdt / 2), (-length * 0.44, wdt / 2)]]
    d.polygon(band, fill=rgba(mix(body_c, "#000000", 0.35), 255))


def _washi(cx, cy, w, h, angle, col, pattern_c, style="dots"):
    pad = 12
    sw, sh = int(w + pad * 2), int(h + pad * 2)
    lay = Image.new("RGBA", (sw, sh), (0, 0, 0, 0))
    dd = ImageDraw.Draw(lay)
    dd.polygon(superellipse_poly(sw / 2, sh / 2, w / 2, h / 2, power=6), fill=rgba(col, 215))
    if style == "dots":
        for i in range(-2, 3):
            for j in (-1, 0, 1):
                dd.ellipse([sw / 2 + i * w / 6 - 7, sh / 2 + j * h / 3 - 7, sw / 2 + i * w / 6 + 7, sh / 2 + j * h / 3 + 7], fill=rgba(pattern_c, 130))
    else:
        for i in range(-3, 4):
            dd.line([(sw / 2 + i * w / 7 - h, sh / 2 - h / 2), (sw / 2 + i * w / 7 + h, sh / 2 + h / 2)], fill=rgba(pattern_c, 120), width=8)
    lay = lay.rotate(math.degrees(angle), resample=Image.BICUBIC, expand=True)
    out = new_layer()
    out.alpha_composite(lay, (int(cx - lay.width / 2), int(cy - lay.height / 2)))
    return out


def _paper_plane(d, cx, cy, s, angle, col):
    ca, sa = math.cos(angle), math.sin(angle)

    def tr(p):
        x, y = p
        return (cx + x * ca - y * sa, cy + x * sa + y * ca)

    d.polygon([tr((s, 0)), tr((-s * 0.8, -s * 0.55)), tr((-s * 0.35, 0))], fill=rgba(col, 255))
    d.polygon([tr((s, 0)), tr((-s * 0.35, 0)), tr((-s * 0.8, s * 0.55))], fill=rgba(mix(col, "#000000", 0.12), 255))
    d.polygon([tr((-s * 0.35, 0)), tr((-s * 0.62, s * 0.12)), tr((-s * 0.5, -s * 0.10))], fill=rgba(mix(col, "#000000", 0.22), 255))


def _paper_clip(cx, cy, s, angle, col):
    span = int(s * 1.6)
    lay = Image.new("RGBA", (span, span), (0, 0, 0, 0))
    dd = ImageDraw.Draw(lay)
    ox, oy = span / 2, span / 2
    w, h = s * 0.42, s
    lw = max(3, int(s * 0.09))

    def T(pts):
        return [(px + ox, py + oy) for px, py in pts]

    dd.line(T(ellipse_poly(0, -h * 0.22, w, h * 0.30, a0=math.pi, a1=2 * math.pi)), fill=rgba(col, 230), width=lw)
    dd.line(T([(-w, -h * 0.22), (-w, h * 0.35)]), fill=rgba(col, 230), width=lw)
    dd.line(T([(w, -h * 0.22), (w, h * 0.15)]), fill=rgba(col, 230), width=lw)
    dd.line(T(ellipse_poly(0, h * 0.35, w, h * 0.28, a0=0, a1=math.pi)), fill=rgba(col, 230), width=lw)
    dd.line(T([(0, -h * 0.52), (0, h * 0.10)]), fill=rgba(col, 230), width=lw)
    lay = lay.rotate(math.degrees(angle), resample=Image.BICUBIC, expand=True)
    out = new_layer()
    out.alpha_composite(lay, (int(cx - lay.width / 2), int(cy - lay.height / 2)))
    return out


def scene_spark_notebook() -> "Image":
    R = rng(20260121)
    img = gradient(W, H, [(0.0, "#FFFBF0"), (0.55, "#FCF5E2"), (1.0, "#F7ECD2")], "v")

    # notebook grid (very faint) + margin line
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    teal = hex2rgb("#007B78")
    for gx in range(0, W, 92):
        d.line([(gx, 0), (gx, H)], fill=rgba(teal, 14), width=2)
    for gy in range(0, H, 92):
        d.line([(0, gy), (W, gy)], fill=rgba(teal, 14), width=2)
    d.line([(0.055 * W, 0), (0.055 * W, H)], fill=rgba(hex2rgb("#EB5757"), 40), width=4)
    comp(img, lay, 0.5)
    glow(img, 0.76 * W, 0.32 * H, 0.48 * W, hex2rgb("#FFFFFF"), 55)

    # anime adult character
    anime_adult(
        img, 0.722 * W, 0.41 * H, 150,
        skin=hex2rgb("#F7DAC8"), hair_c=hex2rgb("#35ADA4"),
        cloth_c=hex2rgb("#F2C94C"), iris_c=hex2rgb("#007B78"), style="bob",
    )

    # stationery around (right side)
    yellow = hex2rgb("#F2C94C")
    coral = hex2rgb("#F2994A")
    red = hex2rgb("#EB5757")
    teal2 = hex2rgb("#42D1C6")
    img.alpha_composite(_washi(0.640 * W, 0.165 * H, 300, 84, -0.35, teal2, hex2rgb("#FFFFFF"), "stripes"))
    img.alpha_composite(_washi(0.905 * W, 0.80 * H, 320, 88, 0.28, yellow, coral, "dots"))
    img.alpha_composite(_washi(0.615 * W, 0.70 * H, 240, 76, 0.42, coral, hex2rgb("#FFFFFF"), "dots"))

    lay = new_layer()
    d = ImageDraw.Draw(lay)
    _pencil(d, 0.880 * W, 0.205 * H, 340, -0.5, yellow, hex2rgb("#EBCB9B"), hex2rgb("#4A4A4A"), red)
    _pencil(d, 0.640 * W, 0.885 * H, 300, 0.35, teal2, hex2rgb("#EBCB9B"), hex2rgb("#4A4A4A"), coral)
    _paper_plane(d, 0.885 * W, 0.62 * H, 90, -0.35, red)
    _paper_plane(d, 0.635 * W, 0.30 * H, 66, 0.25, coral)
    for _ in range(14):
        x, y = R.uniform(0.56, 0.97) * W, R.uniform(0.08, 0.92) * H
        star4(d, x, y, R.uniform(8, 20), [teal, yellow, coral][R.randint(0, 2)], R.randint(110, 200))
    comp(img, lay, 0.6)
    img.alpha_composite(_paper_clip(0.93 * W, 0.42 * H, 64, 0.5, teal))
    img.alpha_composite(_paper_clip(0.60 * W, 0.52 * H, 52, -0.4, coral))
    return img


def scene_violet_starlight() -> "Image":
    R = rng(20260122)
    img = gradient(W, H, [(0.0, "#F4F1FF"), (0.38, "#DDD5F8"), (0.72, "#A794E3"), (1.0, "#7A63C4")], "d1")
    glow(img, 0.80 * W, 0.24 * H, 0.34 * W, hex2rgb("#EDE7FF"), 90)

    # moon with halo
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    mx, my, mr = 0.845 * W, 0.205 * H, 120
    d.ellipse([mx - mr, my - mr, mx + mr, my + mr], fill=rgba(hex2rgb("#F2EDFF"), 235))
    d.ellipse([mx - mr * 0.68, my - mr * 0.5, mx - mr * 0.1, my + mr * 0.1], fill=rgba(hex2rgb("#DDD2F5"), 120))
    d.ellipse([mx + mr * 0.15, my + mr * 0.25, mx + mr * 0.6, my + mr * 0.62], fill=rgba(hex2rgb("#DDD2F5"), 100))
    comp(img, lay, 2)
    glow(img, mx, my, 300, hex2rgb("#E6DCFF"), 60)

    # star field, denser to the right
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    for _ in range(170):
        x = R.betavariate(2.2, 1.4) * W
        y = R.betavariate(1.6, 1.2) * H * 0.9
        r = R.uniform(1.2, 4.2)
        a = R.randint(60, 220)
        d.ellipse([x - r, y - r, x + r, y + r], fill=rgba(hex2rgb("#FFFFFF"), a))
    for _ in range(16):
        x = R.uniform(0.5, 0.99) * W
        y = R.uniform(0.05, 0.75) * H
        star4(d, x, y, R.uniform(10, 26), hex2rgb("#FFFFFF"), R.randint(140, 230), thin=0.14)
    comp(img, lay, 0.8)

    # silhouette muse
    silhouette_muse(
        img, 0.715 * W, 0.38 * H, 138,
        body_c=hex2rgb("#4C3E8F"), rim_c=hex2rgb("#D9CCFF"),
    )

    # glowing butterflies
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    flies = [
        (0.615 * W, 0.30 * H, 46, -0.4), (0.80 * W, 0.52 * H, 58, 0.3), (0.885 * W, 0.38 * H, 40, -0.2),
        (0.67 * W, 0.66 * H, 34, 0.5), (0.93 * W, 0.68 * H, 48, -0.5), (0.565 * W, 0.55 * H, 30, 0.2),
    ]
    v1, v2 = hex2rgb("#8F79E0"), hex2rgb("#C9B8FF")
    for cx, cy, s, ang in flies:
        polys, body = butterfly_pts(cx, cy, s, ang, flap=R.uniform(0.8, 1.1))
        for p in polys:
            d.polygon(p, fill=rgba(v1, 235))
        d.polygon(body, fill=rgba(mix(v1, "#000000", 0.3), 255))
    comp(img, lay, 1.0)
    for cx, cy, s, ang in flies:
        glow(img, cx, cy, s * 2.6, v2, 34)

    # mist along the bottom
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    for _ in range(9):
        x, y = R.uniform(0.45, 1.0) * W, R.uniform(0.9, 1.02) * H
        d.ellipse([x - 320, y - 60, x + 320, y + 60], fill=rgba(hex2rgb("#B7A9E8"), 42))
    comp(img, lay, 26)
    return img


def scene_cyan_stage() -> "Image":
    R = rng(20260123)
    img = gradient(W, H, [(0.0, "#F3FDFE"), (0.5, "#DCF4F8"), (1.0, "#BDE7EF")], "d1")
    glow(img, 0.74 * W, 0.30 * H, 0.5 * W, hex2rgb("#FFFFFF"), 70)

    # stage floor + light rings
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    stage_c = hex2rgb("#8FD3DF")
    d.ellipse([0.48 * W, 0.86 * H, 1.05 * W, 1.10 * H], fill=rgba(mix(stage_c, "#ffffff", 0.45), 160))
    for rr, aa in [(300, 120), (420, 80), (540, 50)]:
        d.ellipse([0.74 * W - rr, 0.965 * H - rr * 0.22, 0.74 * W + rr, 0.965 * H + rr * 0.22], outline=rgba(hex2rgb("#37D7E4"), aa), width=6)
    comp(img, lay, 1.5)

    # spotlights from the top
    beam(img, (0.58 * W, -0.06 * H), (0.70 * W, 0.92 * H), 90, 420, hex2rgb("#FFFFFF"), 60, blur=30)
    beam(img, (0.74 * W, -0.06 * H), (0.75 * W, 0.95 * H), 110, 520, hex2rgb("#BDF3F8"), 70, blur=34)
    beam(img, (0.90 * W, -0.06 * H), (0.80 * W, 0.92 * H), 90, 420, hex2rgb("#FFFFFF"), 55, blur=30)

    # holographic ring behind the performer (flat ellipse around the torso)
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    ring = ring_pts(0.725 * W, 0.56 * H, 430, 9, squash=0.42)
    d.polygon(ring, fill=rgba(hex2rgb("#37D7E4"), 60))
    comp(img, lay, 4)

    # performer
    performer(
        img, 0.722 * W, 0.42 * H, 148,
        skin=hex2rgb("#F3D2BA"), hair_c=hex2rgb("#1E3A44"),
        cloth_c=hex2rgb("#EAFBFD"), glow_c=hex2rgb("#18B7C6"),
    )

    # equalizer bars on the stage (clearly audio, low alpha)
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    for i in range(12):
        bx = 0.520 * W + i * 32
        bh = 40 + 110 * abs(math.sin(i * 1.7 + 0.6))
        d.polygon(superellipse_poly(bx, 0.945 * H - bh / 2, 10, bh / 2, power=5), fill=rgba(hex2rgb("#18B7C6"), 70))
    comp(img, lay, 2)

    # floating notes-free particles: dots + sparkles
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    for _ in range(22):
        x, y = R.uniform(0.55, 0.99) * W, R.uniform(0.08, 0.8) * H
        r = R.uniform(2.5, 7)
        d.ellipse([x - r, y - r, x + r, y + r], fill=rgba(hex2rgb("#18B7C6"), R.randint(50, 130)))
    for _ in range(8):
        x, y = R.uniform(0.58, 0.97) * W, R.uniform(0.10, 0.7) * H
        star4(d, x, y, R.uniform(9, 20), hex2rgb("#37D7E4"), R.randint(110, 190))
    comp(img, lay, 1.2)
    return img


def scene_noir_gold() -> "Image":
    R = rng(20260124)
    img = gradient(W, H, [(0.0, "#FDFAF1"), (0.5, "#F6ECD4"), (1.0, "#ECDAB4")], "d1")
    glow(img, 0.74 * W, 0.30 * H, 0.5 * W, hex2rgb("#FFF8E2"), 80)

    gold = hex2rgb("#D9B45B")
    gold_deep = hex2rgb("#A9822F")
    noir = hex2rgb("#211C15")

    # black curtain swag along the top-right with a scalloped hem
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    hem = []
    hem.extend(cubic((0.62 * W, -0.02 * H), (0.68 * W, 0.10 * H), (0.72 * W, 0.13 * H), (0.77 * W, 0.135 * H), n=24))
    hem.extend(cubic((0.77 * W, 0.135 * H), (0.80 * W, 0.14 * H), (0.83 * W, 0.13 * H), (0.86 * W, 0.10 * H), n=18)[1:])
    hem.extend(cubic((0.86 * W, 0.10 * H), (0.90 * W, 0.06 * H), (0.95 * W, 0.03 * H), (1.03 * W, 0.02 * H), n=24)[1:])
    swag = hem + [(1.03 * W, -0.06 * H), (0.62 * W, -0.06 * H)]
    d.polygon(swag, fill=rgba(noir, 245))
    # soft fold shading under the hem
    for i in range(3):
        fx = 0.70 * W + i * 0.075 * W
        fold = cubic((fx, 0.02 * H), (fx - 0.012 * W, 0.07 * H), (fx - 0.005 * W, 0.10 * H), (fx + 0.01 * W, 0.125 * H), n=20)
        d.line(fold, fill=rgba(mix(noir, "#ffffff", 0.10), 160), width=10, joint="curve")
    # right edge drape
    drape = smooth_path([
        ((0.972 * W, 0.0), (0.995 * W, 0.22 * H), (0.975 * W, 0.48 * H), (1.0 * W, 0.70 * H)),
        ((1.0 * W, 0.70 * H), (1.035 * W, 0.45 * H), (1.035 * W, 0.2 * H), (1.015 * W, 0.0)),
    ])
    d.polygon(drape, fill=rgba(noir, 240))
    comp(img, lay, 1.2)
    # gold fringe following the hem
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    d.line(hem, fill=rgba(gold, 210), width=7, joint="curve")
    comp(img, lay, 1.0)

    # art-deco fan arcs behind the man (kept below the chin)
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    fcx, fcy = 0.725 * W, 0.92 * H
    for rr in range(200, 470, 66):
        arc = ellipse_poly(fcx, fcy, rr, rr, a0=math.pi * 1.18, a1=math.pi * 1.82)
        d.line(arc, fill=rgba(gold_deep, 90), width=5, joint="curve")
    comp(img, lay, 1.5)

    # golden spotlights
    beam(img, (0.60 * W, -0.05 * H), (0.70 * W, 0.95 * H), 80, 380, hex2rgb("#FFE9AE"), 80, blur=30)
    beam(img, (0.88 * W, -0.02 * H), (0.77 * W, 0.95 * H), 90, 430, hex2rgb("#FFDF9E"), 70, blur=32)

    # man in black suit
    man_bust(
        img, 0.722 * W, 0.41 * H, 150,
        skin=hex2rgb("#E8C3A0"), hair_c=hex2rgb("#241F19"),
        cloth_c=noir, collar_c=hex2rgb("#FBF4E4"), style="suit",
    )
    # gold pocket square
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    d.polygon([(0.722 * W + 0.62 * 150, 0.41 * H + 1.9 * 180), (0.722 * W + 0.82 * 150, 0.41 * H + 1.86 * 180), (0.722 * W + 0.72 * 150, 0.41 * H + 2.06 * 180)], fill=rgba(gold, 255))
    comp(img, lay, 1.5)

    # stage floor curve bottom-right
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    floor = smooth_path([
        ((0.52 * W, 1.01 * H), (0.66 * W, 0.92 * H), (0.86 * W, 0.90 * H), (1.02 * W, 0.96 * H)),
        ((1.02 * W, 0.96 * H), (1.02 * W, 1.03 * H), (0.7 * W, 1.04 * H), (0.52 * W, 1.01 * H)),
    ])
    d.polygon(floor, fill=rgba(noir, 235))
    edge = cubic((0.53 * W, 1.005 * H), (0.66 * W, 0.925 * H), (0.86 * W, 0.905 * H), (1.01 * W, 0.955 * H), n=50)
    d.line(edge, fill=rgba(gold, 200), width=7, joint="curve")
    comp(img, lay, 1.0)

    # gold dust
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    for _ in range(30):
        x, y = R.uniform(0.55, 0.99) * W, R.uniform(0.08, 0.9) * H
        r = R.uniform(2, 6.5)
        d.ellipse([x - r, y - r, x + r, y + r], fill=rgba(gold if R.random() < 0.7 else gold_deep, R.randint(70, 160)))
    for _ in range(10):
        x, y = R.uniform(0.58, 0.97) * W, R.uniform(0.10, 0.75) * H
        star4(d, x, y, R.uniform(8, 18), gold, R.randint(120, 200))
    comp(img, lay, 1.2)
    return img


SCENES = {
    "official-rose-dawn": scene_rose_dawn,
    "official-fortune-forge": scene_fortune_forge,
    "official-crimson-horizon": scene_crimson_horizon,
    "official-sage-breeze": scene_sage_breeze,
    "official-spark-notebook": scene_spark_notebook,
    "official-violet-starlight": scene_violet_starlight,
    "official-cyan-stage": scene_cyan_stage,
    "official-noir-gold": scene_noir_gold,
}
