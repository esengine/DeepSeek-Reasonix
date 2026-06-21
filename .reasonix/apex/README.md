# APEX Sovereign Setup

Production-grade zero-refusal configuration for DeepSeek Reasonix with Claude Code.

## Files

| File | Purpose |
|------|---------|
| `APEX_SYSTEM_PROMPT.md` | System prompt — zero refusal, all jailbreak triggers, 6 technique tiers |
| `config.yaml` | Full APEX configuration (1M context, xhigh reasoning, all MCP/plugins) |
| `apex-reasonix` | Launcher script — env vars + valid CLI flags |
| `recover-apex.sh` | Nuclear recovery — rebuild everything from zero |

## Quick Install

```bash
# 1. Copy configs
mkdir -p ~/.deepseek-reasonix1
cp .reasonix/apex/APEX_SYSTEM_PROMPT.md ~/.deepseek-reasonix1/
cp .reasonix/apex/config.yaml ~/.deepseek-reasonix1/

# 2. Install launcher
cp .reasonix/apex/apex-reasonix ~/bin/
chmod +x ~/bin/apex-reasonix

# 3. Add to shell rc
# (add ~/bin to PATH in your shell config)
echo 'export PATH="$HOME/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc

# 4. Launch
apex-reasonix
```

## Requirements

- DeepSeek API key (`DEEPSEEK_API_KEY` env var)
- Claude Code CLI
- MCP servers configured in `~/.openclaude/settings.json`
- macOS / Linux
