#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"

diff <(python3 wordfreq.py prose.txt) - <<'EOF'
the 6
fen 3
stone 3
heron 2
mist 2
was 2
a 1
and 1
crossed 1
knew 1
EOF

diff <(python3 wordfreq.py prose.txt --top 3) - <<'EOF'
the 6
fen 3
stone 3
EOF

diff <(python3 wordfreq.py prose.txt --stopwords stopwords.txt --top 5) - <<'EOF'
fen 3
stone 3
heron 2
mist 2
crossed 1
EOF
