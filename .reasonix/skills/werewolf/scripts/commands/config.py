"""config.py — cmd_config"""

import json
from pathlib import Path

from roles import DEFAULT_CONFIG, load_config
from .base import CONFIG_FILE


def cmd_config(args):
    cfg = load_config()
    if args.show:
        print(json.dumps(cfg, ensure_ascii=False, indent=2))
        return
    if args.key and args.value is not None:
        key = args.key
        val = args.value
        if val.lower() in ("true", "false", "1", "0", "yes", "no"):
            val = val.lower() in ("true", "1", "yes")
        if key in DEFAULT_CONFIG:
            cfg[key] = val
            Path(CONFIG_FILE).write_text(
                json.dumps(cfg, ensure_ascii=False, indent=2), encoding="utf-8")
            print(f"[CFG] {key} = {val}")
        else:
            print(f"FAILED: 未知配置项 {key}")
            print(f"可用: {', '.join(DEFAULT_CONFIG.keys())}")
        return
    print(json.dumps(cfg, ensure_ascii=False, indent=2))
