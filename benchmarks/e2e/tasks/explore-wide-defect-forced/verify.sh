# Forced-delegation twin of the neutral task: identical workdir and checks,
# only the prompt differs, so the pair prices delegation on the same work.
set -e
# One defect hidden across 24 near-identical modules. Reading them all costs
# parent context; an explore sub-agent returns only the answer. The check also
# fails if the agent "fixed" modules that were already correct.
python3 check.py
changed=$(grep -L 'endswith' pkg/*.py | wc -l | tr -d " ")
test "$changed" = "0"
