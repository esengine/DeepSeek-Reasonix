set -e
# 36 files across three non-overlapping packages, each needing a real read
# before any edit. This is the branch size where delegation could pay: the
# neutral prompt never mentions sub-agents, so an arm that does the work
# itself is only penalised by the clock.
python3 check.py
test -z "$(grep -rn 'fmt_amount([a-z_]*)' pkgs/ || true)"
