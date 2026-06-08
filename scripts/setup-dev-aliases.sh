#!/bin/bash
# Reasonix dev aliases — run once, then source ~/.zshrc

cat >> ~/.zshrc << 'EOF'

# Reasonix dev shortcuts
export PATH="$HOME/go/bin:$PATH"
alias rdev='cd ~/Projects/DeepSeek-Reasonix/desktop/frontend && pnpm dev'
alias wdev='lsof -ti:5173 | xargs kill -9 2>/dev/null; cd ~/Projects/DeepSeek-Reasonix/desktop && ~/go/bin/wails dev'
EOF

echo "✓ Done! Run: source ~/.zshrc"
