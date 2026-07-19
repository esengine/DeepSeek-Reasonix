"""Official theme palettes + WCAG contrast gate.

Defines the full token set for all eight official themes and checks the same
pairs the Go backend validates (fg/bg, fg/chat >= 4.5; fgFaint/bg >= 3.0;
accentFg/accent >= 3.0). ok/warn/err are intentionally unset so they inherit
the base style. `write_manifests()` emits theme.json files.
"""
from __future__ import annotations

import json
import os
import shutil
import sys

HERE = os.path.dirname(__file__)
OUT = os.path.join(HERE, "out")
DEST = os.path.join(HERE, "..", "..", "desktop", "themes", "official")

SURFACE_KEYS = ["bg", "bgSoft", "bgElev", "panel", "sidebar", "chat", "workspace", "workspaceFiles"]
TEXT_KEYS = ["border", "borderSoft", "fg", "fgDim", "fgFaint", "accent", "accentFg"]

# focusX, focusY / homeOpacity / taskOpacity / overlayStrength from the plan table.
THEMES = {
    "official-rose-dawn": {
        "name": "Rose Dawn",
        "zh": "玫瑰晨光",
        "baseStyle": "graphite",
        "corners": "round",
        "bgparams": (0.72, 0.43, 1.0, 0.20, 0.68),
        "description": "Ivory dawn light, soft roses and an original illustrated muse on the right.",
        "light": {
            "bg": "#FFF7F8", "bgSoft": "#FBEFF1", "bgElev": "#FFFFFF", "panel": "#FFFFFF",
            "sidebar": "#FBECEF", "chat": "#FFFCFC", "workspace": "#FFFFFF", "workspaceFiles": "#FDF3F4",
            "border": "#E2BCC6", "borderSoft": "#F5E3E7",
            "fg": "#3A252C", "fgDim": "#6D4A55", "fgFaint": "#A97B87",
            "accent": "#B43F65", "accentFg": "#FFF7F8",
        },
        "dark": {
            "bg": "#1E1419", "bgSoft": "#281B21", "bgElev": "#312329", "panel": "#2A1D24",
            "sidebar": "#251922", "chat": "#231820", "workspace": "#312329", "workspaceFiles": "#281C22",
            "border": "#4C3440", "borderSoft": "#3A2832",
            "fg": "#FFF3F6", "fgDim": "#D9B3C0", "fgFaint": "#A87C8C",
            "accent": "#E26D91", "accentFg": "#2A121D",
        },
    },
    "official-fortune-forge": {
        "name": "Fortune Forge",
        "zh": "鸿运工坊",
        "baseStyle": "amber",
        "corners": "soft",
        "bgparams": (0.74, 0.44, 1.0, 0.20, 0.70),
        "description": "Warm ivory workshop with vermilion, gold and jade around an original lucky programmer.",
        "light": {
            "bg": "#FFF8E8", "bgSoft": "#F9EFD5", "bgElev": "#FFFDF6", "panel": "#FFFDF6",
            "sidebar": "#F8EDD3", "chat": "#FFFBF1", "workspace": "#FFFDF6", "workspaceFiles": "#FAF1DC",
            "border": "#D6B87E", "borderSoft": "#F2E7CD",
            "fg": "#382116", "fgDim": "#6E4E35", "fgFaint": "#A98A68",
            "accent": "#A92D22", "accentFg": "#FFF8E8",
        },
        "dark": {
            "bg": "#1D140D", "bgSoft": "#271C12", "bgElev": "#302417", "panel": "#291E13",
            "sidebar": "#251B11", "chat": "#231A11", "workspace": "#302417", "workspaceFiles": "#271D13",
            "border": "#4D3A22", "borderSoft": "#3B2C1A",
            "fg": "#FFF2D1", "fgDim": "#DDC49C", "fgFaint": "#A9926B",
            "accent": "#E8AD38", "accentFg": "#241606",
        },
    },
    "official-crimson-horizon": {
        "name": "Crimson Horizon",
        "zh": "赤曜新城",
        "baseStyle": "graphite",
        "corners": "soft",
        "bgparams": (0.75, 0.45, 0.98, 0.22, 0.66),
        "description": "Pearl-white sky over a coral-red future city skyline. No people, pure skyline.",
        "light": {
            "bg": "#FFF8F7", "bgSoft": "#F9EEEC", "bgElev": "#FFFFFF", "panel": "#FFFFFF",
            "sidebar": "#F9ECEA", "chat": "#FFFBFA", "workspace": "#FFFFFF", "workspaceFiles": "#FBF1EF",
            "border": "#DFB3AD", "borderSoft": "#F6E4E2",
            "fg": "#301D1D", "fgDim": "#6B4644", "fgFaint": "#A9807D",
            "accent": "#B92B38", "accentFg": "#FFF8F7",
        },
        "dark": {
            "bg": "#190D11", "bgSoft": "#221318", "bgElev": "#2B181E", "panel": "#25141A",
            "sidebar": "#211218", "chat": "#201116", "workspace": "#2B181E", "workspaceFiles": "#221319",
            "border": "#582F3C", "borderSoft": "#361B24",
            "fg": "#FFF1F2", "fgDim": "#DFB3B6", "fgFaint": "#AC7E84",
            "accent": "#FF6772", "accentFg": "#2A0E12",
        },
    },
    "official-sage-breeze": {
        "name": "Sage Breeze",
        "zh": "鼠尾草清风",
        "baseStyle": "slate",
        "corners": "soft",
        "bgparams": (0.73, 0.44, 1.0, 0.20, 0.68),
        "description": "Cream paper, sage sprigs and an original illustrated reader on the right.",
        "light": {
            "bg": "#F7F7EF", "bgSoft": "#EFEFE2", "bgElev": "#FCFCF6", "panel": "#FCFCF6",
            "sidebar": "#EEEEE0", "chat": "#FAFAF4", "workspace": "#FCFCF6", "workspaceFiles": "#F1F1E5",
            "border": "#BFC095", "borderSoft": "#E8E8D8",
            "fg": "#26332D", "fgDim": "#4E6157", "fgFaint": "#7C8F83",
            "accent": "#47735F", "accentFg": "#F7F7EF",
        },
        "dark": {
            "bg": "#101814", "bgSoft": "#17211C", "bgElev": "#1E2922", "panel": "#19231D",
            "sidebar": "#161F1A", "chat": "#151E19", "workspace": "#1E2922", "workspaceFiles": "#17211C",
            "border": "#32443A", "borderSoft": "#25322B",
            "fg": "#EEF6F0", "fgDim": "#B7CDBF", "fgFaint": "#7E968A",
            "accent": "#84CBA7", "accentFg": "#0E1A13",
        },
    },
    "official-spark-notebook": {
        "name": "Spark Notebook",
        "zh": "灵感手账",
        "baseStyle": "aurora",
        "corners": "round",
        "bgparams": (0.74, 0.46, 0.98, 0.20, 0.68),
        "description": "Off-white notebook grid with teal, yellow and coral stationery around an original anime adult.",
        "light": {
            "bg": "#FFF9ED", "bgSoft": "#F9F1DE", "bgElev": "#FFFDF7", "panel": "#FFFDF7",
            "sidebar": "#F8F0DC", "chat": "#FFFCF4", "workspace": "#FFFDF7", "workspaceFiles": "#FAF3E2",
            "border": "#D2BC8E", "borderSoft": "#F2E9D4",
            "fg": "#2B2F35", "fgDim": "#565D66", "fgFaint": "#848D98",
            "accent": "#007B78", "accentFg": "#F3FBFA",
        },
        "dark": {
            "bg": "#14171A", "bgSoft": "#1B1F23", "bgElev": "#23282D", "panel": "#1D2226",
            "sidebar": "#1A1E22", "chat": "#191D21", "workspace": "#23282D", "workspaceFiles": "#1B2024",
            "border": "#38404A", "borderSoft": "#2A3138",
            "fg": "#F8F5E9", "fgDim": "#C9CCBF", "fgFaint": "#8B918A",
            "accent": "#42D1C6", "accentFg": "#08201E",
        },
    },
    "official-violet-starlight": {
        "name": "Violet Starlight",
        "zh": "紫曜星夜",
        "baseStyle": "nocturne",
        "corners": "round",
        "bgparams": (0.73, 0.44, 0.96, 0.18, 0.72),
        "description": "Blue-violet starfield with glowing butterflies and an original silhouette muse.",
        "light": {
            "bg": "#F7F4FF", "bgSoft": "#EFEBFB", "bgElev": "#FCFAFF", "panel": "#FCFAFF",
            "sidebar": "#EEE9FA", "chat": "#FAF8FF", "workspace": "#FCFAFF", "workspaceFiles": "#F1EDFB",
            "border": "#CABDEC", "borderSoft": "#E8E3F7",
            "fg": "#251F3C", "fgDim": "#544B74", "fgFaint": "#9188B3",
            "accent": "#6242C7", "accentFg": "#F7F4FF",
        },
        "dark": {
            "bg": "#0C1022", "bgSoft": "#131834", "bgElev": "#1A2140", "panel": "#151B36",
            "sidebar": "#121732", "chat": "#111630", "workspace": "#1A2140", "workspaceFiles": "#131834",
            "border": "#2E3A66", "borderSoft": "#222B4E",
            "fg": "#F4F2FF", "fgDim": "#C2BDE8", "fgFaint": "#8580B8",
            "accent": "#9B86FF", "accentFg": "#14102E",
        },
    },
    "official-cyan-stage": {
        "name": "Cyan Stage",
        "zh": "青岚舞台",
        "baseStyle": "carbon",
        "corners": "round",
        "bgparams": (0.74, 0.45, 0.96, 0.18, 0.72),
        "description": "Airy cyan future stage with light rings and an original digital performer.",
        "light": {
            "bg": "#F1FCFD", "bgSoft": "#E4F5F7", "bgElev": "#FAFEFE", "panel": "#FAFEFE",
            "sidebar": "#E3F4F6", "chat": "#F6FDFE", "workspace": "#FAFEFE", "workspaceFiles": "#E8F6F8",
            "border": "#9CCFD7", "borderSoft": "#DAEDF0",
            "fg": "#173238", "fgDim": "#43606A", "fgFaint": "#71939D",
            "accent": "#007C92", "accentFg": "#F1FCFD",
        },
        "dark": {
            "bg": "#07181D", "bgSoft": "#0C2229", "bgElev": "#112C34", "panel": "#0E252D",
            "sidebar": "#0B2128", "chat": "#0A2027", "workspace": "#112C34", "workspaceFiles": "#0C232A",
            "border": "#1F4550", "borderSoft": "#16333C",
            "fg": "#E9FCFF", "fgDim": "#AEDBE2", "fgFaint": "#6FA5AF",
            "accent": "#37D7E4", "accentFg": "#04222a",
        },
    },
    "official-noir-gold": {
        "name": "Noir Gold",
        "zh": "黑金序曲",
        "baseStyle": "carbon",
        "corners": "soft",
        "bgparams": (0.73, 0.43, 0.94, 0.18, 0.74),
        "description": "Champagne field, black velvet curtain and gold spotlights around an original gentleman.",
        "light": {
            "bg": "#FCF8EE", "bgSoft": "#F6F0DF", "bgElev": "#FEFBF4", "panel": "#FEFBF4",
            "sidebar": "#F5EFDD", "chat": "#FDFAF2", "workspace": "#FEFBF4", "workspaceFiles": "#F7F1E1",
            "border": "#CCBE94", "borderSoft": "#EFE8D2",
            "fg": "#2A241B", "fgDim": "#5C5340", "fgFaint": "#9A8F74",
            "accent": "#7A5A16", "accentFg": "#FCF8EE",
        },
        "dark": {
            "bg": "#0D0B09", "bgSoft": "#15120E", "bgElev": "#1D1913", "panel": "#171410",
            "sidebar": "#14110D", "chat": "#131009", "workspace": "#1D1913", "workspaceFiles": "#15120E",
            "border": "#463B27", "borderSoft": "#2A2418",
            "fg": "#F8F1DF", "fgDim": "#D6CBAE", "fgFaint": "#968C6E",
            "accent": "#D9B45B", "accentFg": "#1D1503",
        },
    },
}


def _lum(hexs: str) -> float:
    s = hexs.lstrip("#")[:6]
    rgb = [int(s[i : i + 2], 16) / 255 for i in (0, 2, 4)]

    def lin(c):
        return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4

    return 0.2126 * lin(rgb[0]) + 0.7152 * lin(rgb[1]) + 0.0722 * lin(rgb[2])


def ratio(a: str, b: str) -> float:
    la, lb = _lum(a), _lum(b)
    return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)


def check() -> int:
    fails = 0
    for tid, spec in THEMES.items():
        for mode in ("light", "dark"):
            tk = spec[mode]
            pairs = [
                ("fg/bg", tk["fg"], tk["bg"], 4.5),
                ("fg/chat", tk["fg"], tk["chat"], 4.5),
                ("fg/panel", tk["fg"], tk["panel"], 4.5),
                ("fgDim/bg", tk["fgDim"], tk["bg"], 3.0),
                ("fgFaint/bg", tk["fgFaint"], tk["bg"], 3.0),
                ("fgFaint/panel", tk["fgFaint"], tk["panel"], 3.0),
                ("accentFg/accent", tk["accentFg"], tk["accent"], 3.0),
                ("accent/bg", tk["accent"], tk["bg"], 3.0),
                ("border/bg", tk["border"], tk["bg"], 1.6),
            ]
            for name, a, b, minimum in pairs:
                r = ratio(a, b)
                if r < minimum:
                    fails += 1
                    print(f"FAIL {tid} {mode} {name}: {r:.2f} < {minimum} ({a} on {b})")
    print("contrast gate:", "OK — zero warnings" if fails == 0 else f"{fails} failures")
    return fails


def write_manifests():
    for tid, spec in THEMES.items():
        fx, fy, home, task, overlay = spec["bgparams"]
        manifest = {
            "schemaVersion": 1,
            "id": tid,
            "name": spec["name"],
            "author": "Reasonix Contributors",
            "description": spec["description"],
            "license": "MIT",
            "baseStyle": spec["baseStyle"],
            "tokens": {"light": spec["light"], "dark": spec["dark"]},
            "recipes": {"density": "comfortable", "corners": spec["corners"]},
            "background": {
                "image": "background.webp",
                "focusX": fx,
                "focusY": fy,
                "safeArea": "left",
                "homeOpacity": home,
                "taskOpacity": task,
                "overlayStrength": overlay,
            },
        }
        tdir = os.path.join(DEST, tid)
        os.makedirs(tdir, exist_ok=True)
        with open(os.path.join(tdir, "theme.json"), "w") as f:
            json.dump(manifest, f, indent=2, ensure_ascii=False)
            f.write("\n")
        for name in ("background.webp", "preview.webp"):
            src = os.path.join(OUT, tid, name)
            shutil.copyfile(src, os.path.join(tdir, name))
        print(f"wrote {tdir}")


if __name__ == "__main__":
    bad = check()
    if bad:
        sys.exit(1)
    if "--write" in sys.argv:
        write_manifests()
