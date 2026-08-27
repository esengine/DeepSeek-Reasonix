#!/usr/bin/env bash
# Telegram Setup für Reasonix
# Ufm Laptop usfüere (wo Reasonix Desktop installiert isch)
set -euo pipefail

CONFIG="${HOME}/.reasonix/config.json"

if [ ! -f "$CONFIG" ]; then
  echo "{\"telegram\":{}}" > "$CONFIG"
  echo "📝 Config erstellt: $CONFIG"
fi

read -rp "🔑 Telegram Bot Token (vo @BotFather): " TOKEN

# Bot Token validiere (sollte format: 123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11)
if [[ ! "$TOKEN" =~ ^[0-9]+:[a-zA-Z0-9_-]+$ ]]; then
  echo "❌ Uugültige Token-Format. Söttet si wie: 123456789:ABCdef123GHI..."
  exit 1
fi

python3 << EOF
import json
path = "$CONFIG"
with open(path) as f:
    cfg = json.load(f)
cfg.setdefault("telegram", {})
cfg["telegram"]["botToken"] = "$TOKEN"
cfg["telegram"]["enabled"] = True
cfg["telegram"]["ownerUserId"] = None
with open(path, "w") as f:
    json.dump(cfg, f, indent=2)
print("✅ Telegram konfiguriert!")
print("   botToken: ${TOKEN:0:8}...")
print("   enabled: true")
EOF

echo ""
echo "🚀 Reasonix nö starte: qr Code → Telegram Channel i Settings → Connect"
