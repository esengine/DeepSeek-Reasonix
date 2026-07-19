"""Original stylised adult figure builders (flat-illustration look).

Every figure is drawn procedurally from bezier primitives — no reference
imagery, no likeness of real persons. All characters are fictional adults.
Coordinates use a local frame: face centre (cx, cy), face half-width fw.
"""
from __future__ import annotations

import math

from PIL import ImageDraw, ImageFilter

from artkit import (
    comp,
    cubic,
    draw_poly,
    ellipse_poly,
    mix,
    new_layer,
    rgba,
    smooth_path,
    soft_fill,
    superellipse_poly,
)


def _face_outline(cx, cy, fw, fh, turn=0.0):
    """Near-frontal face with a soft jaw; turn>0 shifts chin slightly left."""
    chin = (cx - turn * 0.10 * fw, cy + fh)
    segs = [
        # left temple -> crown -> right temple
        ((cx - 0.94 * fw, cy - 0.38 * fh), (cx - 1.02 * fw, cy - 1.02 * fh), (cx + 0.55 * fw, cy - 1.12 * fh), (cx + 0.92 * fw, cy - 0.42 * fh)),
        # right cheek -> right jaw
        ((cx + 0.92 * fw, cy - 0.42 * fh), (cx + 0.98 * fw, cy + 0.18 * fh), (cx + 0.62 * fw, cy + 0.62 * fh), (cx + 0.30 * fw, cy + 0.86 * fh)),
        # right jaw -> chin -> left jaw
        ((cx + 0.30 * fw, cy + 0.86 * fh), (cx + 0.14 * fw, cy + 1.00 * fh), (chin[0] + 0.16 * fw, chin[1] + 0.02 * fh), chin),
        (chin, (chin[0] - 0.34 * fw, chin[1] - 0.02 * fh), (cx - 0.72 * fw, cy + 0.66 * fh), (cx - 0.88 * fw, cy + 0.30 * fh)),
        # left cheek back to temple
        ((cx - 0.88 * fw, cy + 0.30 * fh), (cx - 0.96 * fw, cy + 0.02 * fh), (cx - 0.97 * fw, cy - 0.16 * fh), (cx - 0.94 * fw, cy - 0.38 * fh)),
    ]
    return smooth_path(segs)


def _neck_pts(cx, cy, fw, fh):
    top_y = cy + 0.62 * fh
    bot_y = cy + 1.55 * fh
    return smooth_path([
        ((cx - 0.34 * fw, top_y), (cx - 0.30 * fw, bot_y - 0.2 * fh), (cx - 0.38 * fw, bot_y), (cx - 0.42 * fw, bot_y)),
        ((cx - 0.42 * fw, bot_y), (cx - 0.1 * fw, bot_y + 0.06 * fh), (cx + 0.2 * fw, bot_y + 0.06 * fh), (cx + 0.34 * fw, bot_y)),
        ((cx + 0.34 * fw, bot_y), (cx + 0.28 * fw, bot_y - 0.2 * fh), (cx + 0.30 * fw, top_y), (cx + 0.30 * fw, top_y)),
    ])


def _shoulders_pts(cx, y0, width, height, slope=0.12):
    hw = width / 2
    return smooth_path([
        ((cx - hw, y0 + height), (cx - hw * 1.02, y0 + height * 0.35), (cx - hw * 0.72, y0 + slope * height), (cx - hw * 0.30, y0 + 0.02 * height)),
        ((cx - hw * 0.30, y0 + 0.02 * height), (cx - hw * 0.1, y0 - 0.06 * height), (cx + hw * 0.1, y0 - 0.06 * height), (cx + hw * 0.30, y0 + 0.02 * height)),
        ((cx + hw * 0.30, y0 + 0.02 * height), (cx + hw * 0.72, y0 + slope * height), (cx + hw * 1.02, y0 + height * 0.35), (cx + hw, y0 + height)),
        ((cx + hw, y0 + height), (cx + hw * 0.4, y0 + height * 1.04), (cx - hw * 0.4, y0 + height * 1.04), (cx - hw, y0 + height)),
    ])


def _closed_eye(d, ex, ey, s, color, alpha=235, lashes=0, w=0.16):
    pts = cubic((ex - 0.16 * s, ey), (ex - 0.05 * s, ey + 0.10 * s), (ex + 0.06 * s, ey + 0.10 * s), (ex + 0.17 * s, ey - 0.01 * s), n=24)
    d.line(pts, fill=rgba(color, alpha), width=max(2, int(w * s * 0.28)), joint="curve")
    for i in range(lashes):
        t = 0.55 + 0.22 * i
        bx = ex - 0.16 * s + (0.33 * s) * t
        by = ey + 0.06 * s - 0.05 * s * abs(t - 0.5)
        d.line([(bx, by), (bx + 0.05 * s, by - 0.07 * s)], fill=rgba(color, alpha), width=max(2, int(w * s * 0.2)))


def _open_eye(lay, ex, ey, s, iris, alpha=255, sparkle=True):
    """Simple bright anime-style eye."""
    d = ImageDraw.Draw(lay)
    white = (255, 255, 255)
    d.ellipse([ex - 0.13 * s, ey - 0.17 * s, ex + 0.13 * s, ey + 0.17 * s], fill=rgba(white, alpha))
    d.ellipse([ex - 0.10 * s, ey - 0.13 * s, ex + 0.10 * s, ey + 0.15 * s], fill=rgba(iris, alpha))
    d.ellipse([ex - 0.055 * s, ey - 0.06 * s, ex + 0.055 * s, ey + 0.09 * s], fill=rgba(mix(iris, "#000000", 0.55), alpha))
    if sparkle:
        d.ellipse([ex - 0.055 * s, ey - 0.10 * s, ex + 0.005 * s, ey - 0.04 * s], fill=rgba(white, alpha))
        d.ellipse([ex + 0.03 * s, ey + 0.02 * s, ex + 0.07 * s, ey + 0.06 * s], fill=rgba(white, int(alpha * 0.8)))


def _brow(d, ex, ey, s, color, alpha=200, tilt=0.0):
    pts = cubic((ex - 0.15 * s, ey + tilt), (ex - 0.05 * s, ey - 0.06 * s + tilt), (ex + 0.08 * s, ey - 0.06 * s - tilt), (ex + 0.16 * s, ey - 0.02 * s - tilt), n=20)
    d.line(pts, fill=rgba(color, alpha), width=max(2, int(0.035 * s)), joint="curve")


def _smile(d, cx, y, s, color, alpha=220, width=0.16):
    pts = cubic((cx - width * s, y), (cx - 0.05 * s, y + 0.07 * s), (cx + 0.05 * s, y + 0.07 * s), (cx + width * s, y - 0.01 * s), n=24)
    d.line(pts, fill=rgba(color, alpha), width=max(2, int(0.032 * s)), joint="curve")


def _lips(lay, cx, y, s, color, alpha=230):
    d = ImageDraw.Draw(lay)
    pts = smooth_path([
        ((cx - 0.15 * s, y), (cx - 0.05 * s, y - 0.045 * s), (cx - 0.02 * s, y - 0.03 * s), (cx, y - 0.012 * s)),
        ((cx, y - 0.012 * s), (cx + 0.02 * s, y - 0.03 * s), (cx + 0.05 * s, y - 0.045 * s), (cx + 0.15 * s, y)),
        ((cx + 0.15 * s, y), (cx + 0.07 * s, y + 0.075 * s), (cx - 0.07 * s, y + 0.075 * s), (cx - 0.15 * s, y)),
    ])
    d.polygon(pts, fill=rgba(color, alpha))


def _blush(lay, ex, ey, s, color, alpha=70):
    d = ImageDraw.Draw(lay)
    d.ellipse([ex - 0.16 * s, ey - 0.08 * s, ex + 0.16 * s, ey + 0.08 * s], fill=rgba(color, alpha))


def _figure_shadow(base, cx, cy, fw, fh, scale_w=3.9, scale_h=4.1, alpha=26, blur=52, dx=22, dy=34):
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    d.ellipse(
        [cx - fw * scale_w / 2 + dx, cy - fh * 0.9 + dy, cx + fw * scale_w / 2 + dx, cy + fh * scale_h / 2 + dy],
        fill=(30, 24, 28, alpha),
    )
    comp(base, lay, blur)


# ---------------------------------------------------------------------------
# bust variants


def woman_serene(base, cx, cy, s, skin, hair_c, cloth_c, lip_c, blush_c, style="long", accent=None):
    """Adult woman, gentle closed-eye expression. s = face half-width."""
    fw, fh = s, s * 1.22
    accent = accent or cloth_c
    _figure_shadow(base, cx, cy, fw, fh)

    # back hair mass
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    if style == "long":
        back = smooth_path([
            ((cx - 1.05 * fw, cy - 0.5 * fh), (cx - 1.25 * fw, cy - 1.5 * fh), (cx + 0.9 * fw, cy - 1.65 * fh), (cx + 1.15 * fw, cy - 0.45 * fh)),
            ((cx + 1.15 * fw, cy - 0.45 * fh), (cx + 1.5 * fw, cy + 0.9 * fh), (cx + 1.35 * fw, cy + 2.6 * fh), (cx + 0.75 * fw, cy + 3.1 * fh)),
            ((cx + 0.75 * fw, cy + 3.1 * fh), (cx + 0.45 * fw, cy + 2.2 * fh), (cx + 0.15 * fw, cy + 3.4 * fh), (cx - 0.35 * fw, cy + 3.5 * fh)),
            ((cx - 0.35 * fw, cy + 3.5 * fh), (cx - 1.05 * fw, cy + 2.4 * fh), (cx - 1.35 * fw, cy + 1.0 * fh), (cx - 1.05 * fw, cy - 0.5 * fh)),
        ])
    else:  # bun
        back = smooth_path([
            ((cx - 1.02 * fw, cy - 0.45 * fh), (cx - 1.1 * fw, cy - 1.4 * fh), (cx + 0.8 * fw, cy - 1.5 * fh), (cx + 1.05 * fw, cy - 0.4 * fh)),
            ((cx + 1.05 * fw, cy - 0.4 * fh), (cx + 1.15 * fw, cy + 0.5 * fh), (cx + 0.6 * fw, cy + 1.0 * fh), (cx, cy + 1.05 * fh)),
            ((cx, cy + 1.05 * fh), (cx - 0.7 * fw, cy + 1.0 * fh), (cx - 1.1 * fw, cy + 0.4 * fh), (cx - 1.02 * fw, cy - 0.45 * fh)),
        ])
        d.ellipse([cx + 0.35 * fw, cy - 1.95 * fh, cx + 1.05 * fw, cy - 1.15 * fh], fill=rgba(hair_c, 255))
    d.polygon(back, fill=rgba(hair_c, 255))
    base.alpha_composite(lay)

    # neck + shoulders
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    d.polygon(_neck_pts(cx, cy, fw, fh), fill=rgba(mix(skin, "#000000", 0.07), 255))
    sh = _shoulders_pts(cx, cy + 1.45 * fh, 3.4 * fw, 2.2 * fh)
    d.polygon(sh, fill=rgba(cloth_c, 255))
    # collar V
    d.polygon(
        [(cx - 0.30 * fw, cy + 1.50 * fh), (cx, cy + 1.95 * fh), (cx + 0.30 * fw, cy + 1.50 * fh), (cx + 0.22 * fw, cy + 1.42 * fh), (cx - 0.22 * fw, cy + 1.42 * fh)],
        fill=rgba(mix(cloth_c, "#ffffff", 0.35), 255),
    )
    base.alpha_composite(lay)

    # face
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    d.polygon(_face_outline(cx, cy, fw, fh), fill=rgba(skin, 255))
    base.alpha_composite(lay)

    # fringe / front hair
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    fringe = smooth_path([
        ((cx - 1.0 * fw, cy - 0.18 * fh), (cx - 1.10 * fw, cy - 1.25 * fh), (cx + 0.6 * fw, cy - 1.48 * fh), (cx + 0.98 * fw, cy - 0.42 * fh)),
        ((cx + 0.98 * fw, cy - 0.42 * fh), (cx + 0.72 * fw, cy - 0.58 * fh), (cx + 0.55 * fw, cy - 0.70 * fh), (cx + 0.30 * fw, cy - 0.62 * fh)),
        ((cx + 0.30 * fw, cy - 0.62 * fh), (cx + 0.05 * fw, cy - 0.80 * fh), (cx - 0.35 * fw, cy - 0.78 * fh), (cx - 0.55 * fw, cy - 0.56 * fh)),
        ((cx - 0.55 * fw, cy - 0.56 * fh), (cx - 0.75 * fw, cy - 0.70 * fh), (cx - 0.9 * fw, cy - 0.48 * fh), (cx - 1.0 * fw, cy - 0.18 * fh)),
    ])
    d.polygon(fringe, fill=rgba(hair_c, 255))
    # side strand
    strand = smooth_path([
        ((cx + 0.82 * fw, cy - 0.62 * fh), (cx + 1.10 * fw, cy - 0.05 * fh), (cx + 1.05 * fw, cy + 0.9 * fh), (cx + 0.8 * fw, cy + 1.6 * fh)),
        ((cx + 0.8 * fw, cy + 1.6 * fh), (cx + 0.66 * fw, cy + 0.8 * fh), (cx + 0.64 * fw, cy - 0.05 * fh), (cx + 0.68 * fw, cy - 0.58 * fh)),
    ])
    d.polygon(strand, fill=rgba(mix(hair_c, "#ffffff", 0.06), 255))
    base.alpha_composite(lay)

    # features
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    ink = mix(skin, "#000000", 0.72)
    _closed_eye(d, cx - 0.42 * fw, cy - 0.02 * fh, fw, ink, lashes=2)
    _closed_eye(d, cx + 0.42 * fw, cy - 0.02 * fh, fw, ink, lashes=2)
    _brow(d, cx - 0.42 * fw, cy - 0.30 * fh, fw, mix(hair_c, "#000000", 0.25))
    _brow(d, cx + 0.42 * fw, cy - 0.30 * fh, fw, mix(hair_c, "#000000", 0.25))
    nose = cubic((cx - 0.02 * fw, cy + 0.16 * fh), (cx + 0.03 * fw, cy + 0.24 * fh), (cx + 0.02 * fw, cy + 0.30 * fh), (cx - 0.04 * fw, cy + 0.32 * fh), n=16)
    d.line(nose, fill=rgba(mix(skin, "#000000", 0.30), 200), width=max(2, int(0.03 * fw)), joint="curve")
    _lips(lay, cx + 0.02 * fw, cy + 0.52 * fh, fw, lip_c)
    _blush(lay, cx - 0.52 * fw, cy + 0.30 * fh, fw, blush_c)
    _blush(lay, cx + 0.56 * fw, cy + 0.30 * fh, fw, blush_c)
    base.alpha_composite(lay)


def man_bust(base, cx, cy, s, skin, hair_c, cloth_c, collar_c=None, style="shirt"):
    """Adult man, calm closed-eye expression. style: shirt | suit."""
    fw, fh = s, s * 1.20
    collar_c = collar_c or mix(cloth_c, "#ffffff", 0.5)
    _figure_shadow(base, cx, cy, fw, fh, alpha=40)

    lay = new_layer()
    d = ImageDraw.Draw(lay)
    d.polygon(_neck_pts(cx, cy, fw, fh), fill=rgba(mix(skin, "#000000", 0.08), 255))
    sh = _shoulders_pts(cx, cy + 1.45 * fh, 3.6 * fw, 2.25 * fh)
    d.polygon(sh, fill=rgba(cloth_c, 255))
    if style == "suit":
        # shirt V + lapels + tie
        d.polygon([(cx - 0.38 * fw, cy + 1.5 * fh), (cx, cy + 2.35 * fh), (cx + 0.38 * fw, cy + 1.5 * fh)], fill=rgba(collar_c, 255))
        d.polygon([(cx - 0.09 * fw, cy + 1.62 * fh), (cx + 0.09 * fw, cy + 1.62 * fh), (cx + 0.05 * fw, cy + 1.78 * fh), (cx - 0.05 * fw, cy + 1.78 * fh)], fill=rgba(mix(cloth_c, "#000000", 0.4), 255))
        d.polygon([(cx - 0.05 * fw, cy + 1.78 * fh), (cx + 0.05 * fw, cy + 1.78 * fh), (cx + 0.10 * fw, cy + 2.35 * fh), (cx, cy + 2.5 * fh), (cx - 0.10 * fw, cy + 2.35 * fh)], fill=rgba(mix(cloth_c, "#000000", 0.4), 255))
        lap_l = [(cx - 0.38 * fw, cy + 1.5 * fh), (cx - 0.95 * fw, cy + 1.75 * fh), (cx - 0.55 * fw, cy + 2.6 * fh), (cx - 0.12 * fw, cy + 2.1 * fh)]
        lap_r = [(cx + 0.38 * fw, cy + 1.5 * fh), (cx + 0.95 * fw, cy + 1.75 * fh), (cx + 0.55 * fw, cy + 2.6 * fh), (cx + 0.12 * fw, cy + 2.1 * fh)]
        d.polygon(lap_l, fill=rgba(mix(cloth_c, "#ffffff", 0.08), 255))
        d.polygon(lap_r, fill=rgba(mix(cloth_c, "#ffffff", 0.08), 255))
    else:
        # crew / open collar
        d.polygon([(cx - 0.40 * fw, cy + 1.48 * fh), (cx, cy + 1.95 * fh), (cx + 0.40 * fw, cy + 1.48 * fh), (cx + 0.28 * fw, cy + 1.38 * fh), (cx - 0.28 * fw, cy + 1.38 * fh)], fill=rgba(collar_c, 255))
    base.alpha_composite(lay)

    lay = new_layer()
    d = ImageDraw.Draw(lay)
    d.polygon(_face_outline(cx, cy, fw, fh, turn=0.4), fill=rgba(skin, 255))
    base.alpha_composite(lay)

    # short hair
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    hair = smooth_path([
        ((cx - 0.98 * fw, cy - 0.30 * fh), (cx - 1.05 * fw, cy - 1.18 * fh), (cx + 0.6 * fw, cy - 1.38 * fh), (cx + 0.97 * fw, cy - 0.42 * fh)),
        ((cx + 0.97 * fw, cy - 0.42 * fh), (cx + 0.86 * fw, cy - 0.60 * fh), (cx + 0.68 * fw, cy - 0.74 * fh), (cx + 0.42 * fw, cy - 0.68 * fh)),
        ((cx + 0.42 * fw, cy - 0.68 * fh), (cx + 0.08 * fw, cy - 0.88 * fh), (cx - 0.45 * fw, cy - 0.84 * fh), (cx - 0.68 * fw, cy - 0.58 * fh)),
        ((cx - 0.68 * fw, cy - 0.58 * fh), (cx - 0.86 * fw, cy - 0.68 * fh), (cx - 0.96 * fw, cy - 0.50 * fh), (cx - 0.98 * fw, cy - 0.30 * fh)),
    ])
    d.polygon(hair, fill=rgba(hair_c, 255))
    base.alpha_composite(lay)

    lay = new_layer()
    d = ImageDraw.Draw(lay)
    ink = mix(skin, "#000000", 0.75)
    _closed_eye(d, cx - 0.42 * fw, cy + 0.0 * fh, fw, ink, lashes=0)
    _closed_eye(d, cx + 0.42 * fw, cy + 0.0 * fh, fw, ink, lashes=0)
    _brow(d, cx - 0.42 * fw, cy - 0.28 * fh, fw, mix(hair_c, "#000000", 0.2), tilt=0.01 * fw)
    _brow(d, cx + 0.42 * fw, cy - 0.28 * fh, fw, mix(hair_c, "#000000", 0.2), tilt=0.01 * fw)
    nose = cubic((cx - 0.02 * fw, cy + 0.14 * fh), (cx + 0.035 * fw, cy + 0.24 * fh), (cx + 0.03 * fw, cy + 0.31 * fh), (cx - 0.05 * fw, cy + 0.33 * fh), n=16)
    d.line(nose, fill=rgba(mix(skin, "#000000", 0.32), 200), width=max(2, int(0.032 * fw)), joint="curve")
    _smile(d, cx, cy + 0.52 * fh, fw, mix(skin, "#7a3b3b", 0.55), width=0.15)
    base.alpha_composite(lay)


def anime_adult(base, cx, cy, s, skin, hair_c, cloth_c, iris_c, style="bob"):
    """Original adult anime-style character, bright open eyes."""
    fw, fh = s, s * 1.16
    _figure_shadow(base, cx, cy, fw, fh, alpha=40)

    # back hair
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    if style == "twin":
        for sgn in (-1, 1):
            tail = smooth_path([
                ((cx + sgn * 0.85 * fw, cy - 0.7 * fh), (cx + sgn * 1.9 * fw, cy - 0.2 * fh), (cx + sgn * 2.1 * fw, cy + 1.2 * fh), (cx + sgn * 1.5 * fw, cy + 2.4 * fh)),
                ((cx + sgn * 1.5 * fw, cy + 2.4 * fh), (cx + sgn * 1.2 * fw, cy + 1.4 * fh), (cx + sgn * 0.9 * fw, cy + 0.6 * fh), (cx + sgn * 0.72 * fw, cy - 0.2 * fh)),
            ])
            d.polygon(tail, fill=rgba(mix(hair_c, "#000000", 0.08), 255))
    back = smooth_path([
        ((cx - 1.08 * fw, cy - 0.3 * fh), (cx - 1.2 * fw, cy - 1.5 * fh), (cx + 0.9 * fw, cy - 1.6 * fh), (cx + 1.12 * fw, cy - 0.3 * fh)),
        ((cx + 1.12 * fw, cy - 0.3 * fh), (cx + 1.3 * fw, cy + 0.9 * fh), (cx + 0.95 * fw, cy + 2.0 * fh), (cx + 0.55 * fw, cy + 2.5 * fh)),
        ((cx + 0.55 * fw, cy + 2.5 * fh), (cx + 0.3 * fw, cy + 1.6 * fh), (cx - 0.1 * fw, cy + 2.6 * fh), (cx - 0.5 * fw, cy + 2.4 * fh)),
        ((cx - 0.5 * fw, cy + 2.4 * fh), (cx - 1.15 * fw, cy + 1.2 * fh), (cx - 1.25 * fw, cy + 0.4 * fh), (cx - 1.08 * fw, cy - 0.3 * fh)),
    ])
    d.polygon(back, fill=rgba(hair_c, 255))
    base.alpha_composite(lay)

    lay = new_layer()
    d = ImageDraw.Draw(lay)
    d.polygon(_neck_pts(cx, cy, fw, fh), fill=rgba(mix(skin, "#000000", 0.06), 255))
    sh = _shoulders_pts(cx, cy + 1.42 * fh, 3.1 * fw, 2.1 * fh)
    d.polygon(sh, fill=rgba(cloth_c, 255))
    # hoodie strings + collar
    d.polygon([(cx - 0.44 * fw, cy + 1.5 * fh), (cx, cy + 2.05 * fh), (cx + 0.44 * fw, cy + 1.5 * fh), (cx + 0.30 * fw, cy + 1.38 * fh), (cx - 0.30 * fw, cy + 1.38 * fh)], fill=rgba(mix(cloth_c, "#000000", 0.12), 255))
    d.line([(cx - 0.14 * fw, cy + 1.9 * fh), (cx - 0.16 * fw, cy + 2.3 * fh)], fill=rgba(mix(cloth_c, "#ffffff", 0.6), 255), width=max(2, int(0.045 * fw)))
    d.line([(cx + 0.14 * fw, cy + 1.9 * fh), (cx + 0.16 * fw, cy + 2.3 * fh)], fill=rgba(mix(cloth_c, "#ffffff", 0.6), 255), width=max(2, int(0.045 * fw)))
    base.alpha_composite(lay)

    lay = new_layer()
    d = ImageDraw.Draw(lay)
    d.polygon(_face_outline(cx, cy, fw, fh), fill=rgba(skin, 255))
    base.alpha_composite(lay)

    # fringe with anime points
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    pts = []
    x0, x1 = cx - 1.02 * fw, cx + 1.02 * fw
    top_y = cy - 1.28 * fh
    bot_y = cy - 0.42 * fh
    n = 7
    pts.append((x0, cy - 0.2 * fh))
    pts.extend(cubic((x0, cy - 0.2 * fh), (x0 - 0.05 * fw, top_y - 0.2 * fh), (cx, top_y - 0.25 * fh), (x1, cy - 0.35 * fh), n=30)[1:])
    pts.append((x1, cy - 0.1 * fh))
    for i in range(n):
        xm = x1 - (x1 - x0) * (i + 0.5) / n
        xe = x1 - (x1 - x0) * (i + 1) / n
        depth = bot_y + (0.10 * fh if i % 2 == 0 else -0.06 * fh)
        pts.append((xm, depth))
        pts.append((xe, bot_y - 0.08 * fh))
    d.polygon(pts, fill=rgba(hair_c, 255))
    base.alpha_composite(lay)

    lay = new_layer()
    d = ImageDraw.Draw(lay)
    _open_eye(lay, cx - 0.42 * fw, cy + 0.05 * fh, fw, iris_c)
    _open_eye(lay, cx + 0.42 * fw, cy + 0.05 * fh, fw, iris_c)
    ink = mix(hair_c, "#000000", 0.45)
    d.line(cubic((cx - 0.58 * fw, cy - 0.12 * fh), (cx - 0.45 * fw, cy - 0.22 * fh), (cx - 0.28 * fw, cy - 0.22 * fh), (cx - 0.24 * fw, cy - 0.14 * fh), n=16), fill=rgba(ink, 220), width=max(2, int(0.035 * fw)))
    d.line(cubic((cx + 0.58 * fw, cy - 0.12 * fh), (cx + 0.45 * fw, cy - 0.22 * fh), (cx + 0.28 * fw, cy - 0.22 * fh), (cx + 0.24 * fw, cy - 0.14 * fh), n=16), fill=rgba(ink, 220), width=max(2, int(0.035 * fw)))
    _smile(d, cx, cy + 0.5 * fh, fw, mix(skin, "#8a4040", 0.6), width=0.13)
    _blush(lay, cx - 0.58 * fw, cy + 0.32 * fh, fw, (242, 153, 142), alpha=80)
    _blush(lay, cx + 0.58 * fw, cy + 0.32 * fh, fw, (242, 153, 142), alpha=80)
    base.alpha_composite(lay)


def silhouette_muse(base, cx, cy, s, body_c, rim_c=None):
    """Elegant adult woman silhouette in a flowing gown (no facial features)."""
    fw, fh = s, s * 1.2
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    # flowing gown
    gown = smooth_path([
        ((cx - 0.35 * fw, cy + 1.2 * fh), (cx - 1.6 * fw, cy + 2.2 * fh), (cx - 2.6 * fw, cy + 3.4 * fh), (cx - 2.9 * fw, cy + 4.6 * fh)),
        ((cx - 2.9 * fw, cy + 4.6 * fh), (cx - 1.2 * fw, cy + 4.9 * fh), (cx + 1.4 * fw, cy + 4.9 * fh), (cx + 2.8 * fw, cy + 4.4 * fh)),
        ((cx + 2.8 * fw, cy + 4.4 * fh), (cx + 2.2 * fw, cy + 3.0 * fh), (cx + 1.3 * fw, cy + 2.0 * fh), (cx + 0.38 * fw, cy + 1.2 * fh)),
    ])
    d.polygon(gown, fill=rgba(body_c, 255))
    # torso + neck + head
    d.polygon(_neck_pts(cx, cy, fw, fh), fill=rgba(body_c, 255))
    torso = smooth_path([
        ((cx - 0.9 * fw, cy + 2.2 * fh), (cx - 0.75 * fw, cy + 1.4 * fh), (cx - 0.4 * fw, cy + 1.25 * fh), (cx, cy + 1.2 * fh)),
        ((cx, cy + 1.2 * fh), (cx + 0.4 * fw, cy + 1.25 * fh), (cx + 0.75 * fw, cy + 1.4 * fh), (cx + 0.9 * fw, cy + 2.2 * fh)),
        ((cx + 0.9 * fw, cy + 2.2 * fh), (cx + 0.4 * fw, cy + 2.5 * fh), (cx - 0.4 * fw, cy + 2.5 * fh), (cx - 0.9 * fw, cy + 2.2 * fh)),
    ])
    d.polygon(torso, fill=rgba(body_c, 255))
    d.polygon(_face_outline(cx, cy, fw * 0.92, fh * 0.92), fill=rgba(body_c, 255))
    # flowing hair
    hair = smooth_path([
        ((cx - 0.95 * fw, cy - 0.35 * fh), (cx - 1.1 * fw, cy - 1.5 * fh), (cx + 0.9 * fw, cy - 1.6 * fh), (cx + 1.05 * fw, cy - 0.3 * fh)),
        ((cx + 1.05 * fw, cy - 0.3 * fh), (cx + 1.9 * fw, cy + 0.6 * fh), (cx + 2.6 * fw, cy + 1.1 * fh), (cx + 3.2 * fw, cy + 0.9 * fh)),
        ((cx + 3.2 * fw, cy + 0.9 * fh), (cx + 2.4 * fw, cy + 1.7 * fh), (cx + 1.6 * fw, cy + 1.7 * fh), (cx + 1.0 * fw, cy + 1.3 * fh)),
        ((cx + 1.0 * fw, cy + 1.3 * fh), (cx + 0.5 * fw, cy + 0.9 * fh), (cx - 0.2 * fw, cy + 1.1 * fh), (cx - 0.6 * fw, cy + 0.7 * fh)),
        ((cx - 0.6 * fw, cy + 0.7 * fh), (cx - 1.05 * fw, cy + 0.3 * fh), (cx - 1.0 * fw, cy - 0.05 * fh), (cx - 0.95 * fw, cy - 0.35 * fh)),
    ])
    d.polygon(hair, fill=rgba(mix(body_c, "#000000", 0.12), 255))
    base.alpha_composite(lay)
    if rim_c:
        rim = new_layer()
        rd = ImageDraw.Draw(rim)
        rd.line(cubic((cx + 0.9 * fw, cy - 0.6 * fh), (cx + 1.2 * fw, cy + 0.4 * fh), (cx + 1.1 * fw, cy + 1.6 * fh), (cx + 0.8 * fw, cy + 2.2 * fh), n=40), fill=rgba(rim_c, 150), width=max(3, int(0.07 * fw)))
        rd.line(cubic((cx + 1.05 * fw, cy - 0.3 * fh), (cx + 2.0 * fw, cy + 0.7 * fh), (cx + 2.8 * fw, cy + 1.2 * fh), (cx + 3.1 * fw, cy + 1.0 * fh), n=40), fill=rgba(rim_c, 120), width=max(3, int(0.06 * fw)))
        comp(base, rim, 6)


def mascot_programmer(base, cx, cy, s, skin, hair_c, cloth_c, accent_c, glasses_c=None):
    """Original cheerful 'lucky programmer' mascot — fictional adult coder."""
    fw, fh = s, s * 1.14
    glasses_c = glasses_c or mix(cloth_c, "#000000", 0.5)
    _figure_shadow(base, cx, cy, fw, fh, alpha=42)

    lay = new_layer()
    d = ImageDraw.Draw(lay)
    d.polygon(_neck_pts(cx, cy, fw, fh), fill=rgba(mix(skin, "#000000", 0.07), 255))
    sh = _shoulders_pts(cx, cy + 1.42 * fh, 3.5 * fw, 2.3 * fh)
    d.polygon(sh, fill=rgba(cloth_c, 255))
    # hoodie collar + pocket + strings
    d.polygon([(cx - 0.5 * fw, cy + 1.5 * fh), (cx, cy + 2.0 * fh), (cx + 0.5 * fw, cy + 1.5 * fh), (cx + 0.36 * fw, cy + 1.34 * fh), (cx - 0.36 * fw, cy + 1.34 * fh)], fill=rgba(mix(cloth_c, "#000000", 0.14), 255))
    d.line([(cx - 0.16 * fw, cy + 1.85 * fh), (cx - 0.20 * fw, cy + 2.35 * fh)], fill=rgba(accent_c, 255), width=max(3, int(0.05 * fw)))
    d.line([(cx + 0.16 * fw, cy + 1.85 * fh), (cx + 0.20 * fw, cy + 2.35 * fh)], fill=rgba(accent_c, 255), width=max(3, int(0.05 * fw)))
    pocket = superellipse_poly(cx, cy + 2.9 * fh, 0.7 * fw, 0.5 * fh, power=5)
    d.polygon(pocket, fill=rgba(mix(cloth_c, "#ffffff", 0.10), 255))
    base.alpha_composite(lay)

    # headphones around neck
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    band = cubic((cx - 0.75 * fw, cy + 1.35 * fh), (cx - 0.5 * fw, cy + 1.9 * fh), (cx + 0.5 * fw, cy + 1.9 * fh), (cx + 0.75 * fw, cy + 1.35 * fh), n=40)
    d.line(band, fill=rgba(glasses_c, 255), width=max(4, int(0.14 * fw)))
    for sgn in (-1, 1):
        d.ellipse([cx + sgn * 0.82 * fw - 0.17 * fw, cy + 1.18 * fh, cx + sgn * 0.82 * fw + 0.17 * fw, cy + 1.62 * fh], fill=rgba(glasses_c, 255))
        d.ellipse([cx + sgn * 0.82 * fw - 0.09 * fw, cy + 1.28 * fh, cx + sgn * 0.82 * fw + 0.09 * fw, cy + 1.52 * fh], fill=rgba(accent_c, 255))
    base.alpha_composite(lay)

    lay = new_layer()
    d = ImageDraw.Draw(lay)
    d.polygon(_face_outline(cx, cy, fw, fh), fill=rgba(skin, 255))
    base.alpha_composite(lay)

    # short tidy hair
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    hair = smooth_path([
        ((cx - 0.98 * fw, cy - 0.28 * fh), (cx - 1.07 * fw, cy - 1.22 * fh), (cx + 0.62 * fw, cy - 1.40 * fh), (cx + 0.99 * fw, cy - 0.38 * fh)),
        ((cx + 0.99 * fw, cy - 0.38 * fh), (cx + 0.88 * fw, cy - 0.62 * fh), (cx + 0.58 * fw, cy - 0.80 * fh), (cx + 0.28 * fw, cy - 0.72 * fh)),
        ((cx + 0.28 * fw, cy - 0.72 * fh), (cx - 0.06 * fw, cy - 0.88 * fh), (cx - 0.5 * fw, cy - 0.82 * fh), (cx - 0.70 * fw, cy - 0.54 * fh)),
        ((cx - 0.70 * fw, cy - 0.54 * fh), (cx - 0.88 * fw, cy - 0.64 * fh), (cx - 0.97 * fw, cy - 0.48 * fh), (cx - 0.98 * fw, cy - 0.28 * fh)),
    ])
    d.polygon(hair, fill=rgba(hair_c, 255))
    base.alpha_composite(lay)

    # face: happy closed eyes (^^), round glasses
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    ink = mix(skin, "#000000", 0.78)
    for sgn in (-1, 1):
        ex = cx + sgn * 0.42 * fw
        d.ellipse([ex - 0.26 * fw, cy - 0.20 * fh, ex + 0.26 * fw, cy + 0.30 * fh], outline=rgba(glasses_c, 235), width=max(3, int(0.045 * fw)))
        pts = cubic((ex - 0.14 * fw, cy + 0.08 * fh), (ex - 0.05 * fw, cy - 0.03 * fh), (ex + 0.05 * fw, cy - 0.03 * fh), (ex + 0.14 * fw, cy + 0.08 * fh), n=20)
        d.line(pts, fill=rgba(ink, 240), width=max(3, int(0.045 * fw)), joint="curve")
    d.line([(cx - 0.16 * fw, cy + 0.05 * fh), (cx + 0.16 * fw, cy + 0.05 * fh)], fill=rgba(glasses_c, 235), width=max(3, int(0.04 * fw)))
    d.line([(cx - 0.68 * fw, cy + 0.02 * fh), (cx - 0.88 * fw, cy - 0.05 * fh)], fill=rgba(glasses_c, 235), width=max(3, int(0.04 * fw)))
    d.line([(cx + 0.68 * fw, cy + 0.02 * fh), (cx + 0.88 * fw, cy - 0.05 * fh)], fill=rgba(glasses_c, 235), width=max(3, int(0.04 * fw)))
    # open happy mouth
    mouth = smooth_path([
        ((cx - 0.16 * fw, cy + 0.5 * fh), (cx - 0.05 * fw, cy + 0.52 * fh), (cx + 0.05 * fw, cy + 0.52 * fh), (cx + 0.16 * fw, cy + 0.5 * fh)),
        ((cx + 0.16 * fw, cy + 0.5 * fh), (cx + 0.08 * fw, cy + 0.68 * fh), (cx - 0.08 * fw, cy + 0.68 * fh), (cx - 0.16 * fw, cy + 0.5 * fh)),
    ])
    d.polygon(mouth, fill=rgba(mix(skin, "#7a2e2e", 0.6), 235))
    _blush(lay, cx - 0.62 * fw, cy + 0.34 * fh, fw, (238, 140, 110), alpha=85)
    _blush(lay, cx + 0.62 * fw, cy + 0.34 * fh, fw, (238, 140, 110), alpha=85)
    base.alpha_composite(lay)


def performer(base, cx, cy, s, skin, hair_c, cloth_c, glow_c):
    """Original adult digital stage performer with headset mic, high ponytail."""
    fw, fh = s, s * 1.18
    _figure_shadow(base, cx, cy, fw, fh, alpha=44)

    # ponytail
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    tail = smooth_path([
        ((cx + 0.55 * fw, cy - 1.35 * fh), (cx + 1.6 * fw, cy - 1.2 * fh), (cx + 2.1 * fw, cy + 0.2 * fh), (cx + 1.85 * fw, cy + 1.6 * fh)),
        ((cx + 1.85 * fw, cy + 1.6 * fh), (cx + 1.25 * fw, cy + 0.9 * fh), (cx + 0.85 * fw, cy + 0.2 * fh), (cx + 0.7 * fw, cy - 0.6 * fh)),
    ])
    d.polygon(tail, fill=rgba(mix(hair_c, "#000000", 0.10), 255))
    base.alpha_composite(lay)

    lay = new_layer()
    d = ImageDraw.Draw(lay)
    d.polygon(_neck_pts(cx, cy, fw, fh), fill=rgba(mix(skin, "#000000", 0.07), 255))
    sh = _shoulders_pts(cx, cy + 1.45 * fh, 3.3 * fw, 2.2 * fh)
    d.polygon(sh, fill=rgba(cloth_c, 255))
    # jacket collar wings
    d.polygon([(cx - 0.46 * fw, cy + 1.5 * fh), (cx - 0.12 * fw, cy + 2.1 * fh), (cx - 0.55 * fw, cy + 2.0 * fh), (cx - 0.75 * fw, cy + 1.65 * fh)], fill=rgba(mix(cloth_c, glow_c, 0.25), 255))
    d.polygon([(cx + 0.46 * fw, cy + 1.5 * fh), (cx + 0.12 * fw, cy + 2.1 * fh), (cx + 0.55 * fw, cy + 2.0 * fh), (cx + 0.75 * fw, cy + 1.65 * fh)], fill=rgba(mix(cloth_c, glow_c, 0.25), 255))
    base.alpha_composite(lay)

    lay = new_layer()
    d = ImageDraw.Draw(lay)
    d.polygon(_face_outline(cx, cy, fw, fh), fill=rgba(skin, 255))
    base.alpha_composite(lay)

    # swept hair + fringe
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    hair = smooth_path([
        ((cx - 1.0 * fw, cy - 0.3 * fh), (cx - 1.1 * fw, cy - 1.3 * fh), (cx + 0.7 * fw, cy - 1.5 * fh), (cx + 1.05 * fw, cy - 0.4 * fh)),
        ((cx + 1.05 * fw, cy - 0.4 * fh), (cx + 0.8 * fw, cy - 0.7 * fh), (cx + 0.5 * fw, cy - 0.9 * fh), (cx + 0.2 * fw, cy - 0.82 * fh)),
        ((cx + 0.2 * fw, cy - 0.82 * fh), (cx - 0.2 * fw, cy - 1.0 * fh), (cx - 0.6 * fw, cy - 0.9 * fh), (cx - 0.8 * fw, cy - 0.6 * fh)),
        ((cx - 0.8 * fw, cy - 0.6 * fh), (cx - 0.95 * fw, cy - 0.7 * fh), (cx - 1.0 * fw, cy - 0.5 * fh), (cx - 1.0 * fw, cy - 0.3 * fh)),
    ])
    d.polygon(hair, fill=rgba(hair_c, 255))
    base.alpha_composite(lay)

    # headset: band over crown + mic boom to mouth
    lay = new_layer()
    d = ImageDraw.Draw(lay)
    band = cubic((cx - 0.85 * fw, cy - 0.45 * fh), (cx - 0.6 * fw, cy - 1.45 * fh), (cx + 0.6 * fw, cy - 1.45 * fh), (cx + 0.85 * fw, cy - 0.45 * fh), n=40)
    d.line(band, fill=rgba(mix(cloth_c, "#000000", 0.3), 255), width=max(3, int(0.09 * fw)))
    d.ellipse([cx + 0.72 * fw, cy - 0.28 * fh, cx + 1.02 * fw, cy + 0.12 * fh], fill=rgba(mix(cloth_c, "#000000", 0.3), 255))
    boom = cubic((cx + 0.88 * fw, cy + 0.05 * fh), (cx + 0.95 * fw, cy + 0.45 * fh), (cx + 0.5 * fw, cy + 0.62 * fh), (cx + 0.28 * fw, cy + 0.58 * fh), n=30)
    d.line(boom, fill=rgba(mix(cloth_c, "#000000", 0.3), 255), width=max(2, int(0.05 * fw)))
    d.ellipse([cx + 0.18 * fw, cy + 0.5 * fh, cx + 0.34 * fw, cy + 0.66 * fh], fill=rgba(glow_c, 255))

    ink = mix(skin, "#000000", 0.72)
    _closed_eye(d, cx - 0.42 * fw, cy + 0.0 * fh, fw, ink, lashes=2)
    _open_eye(lay, cx + 0.42 * fw, cy + 0.02 * fh, fw, mix(glow_c, "#000000", 0.25))
    _brow(d, cx - 0.42 * fw, cy - 0.28 * fh, fw, mix(hair_c, "#000000", 0.25))
    _brow(d, cx + 0.42 * fw, cy - 0.28 * fh, fw, mix(hair_c, "#000000", 0.25))
    _smile(d, cx, cy + 0.52 * fh, fw, mix(skin, "#8a4040", 0.6), width=0.15)
    _blush(lay, cx - 0.56 * fw, cy + 0.3 * fh, fw, glow_c, alpha=55)
    _blush(lay, cx + 0.56 * fw, cy + 0.3 * fh, fw, glow_c, alpha=55)
    base.alpha_composite(lay)
