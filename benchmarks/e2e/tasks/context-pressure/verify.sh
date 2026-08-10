# Same migration under an aggressive project compact_ratio (0.5%), an attempt
# to put the parent under real context pressure — the one mechanism delegation
# was designed for. It did not work: the agent keeps its session small by
# writing a script instead of reading, so compaction never engaged and the
# arms measured the same thing the other shapes did. Kept as an honest record
# of the attempt, and as a config variant that still exercises the workspace
# reasonix.toml merge path.
set -e
# 36 files across three non-overlapping packages, each needing a real read
# before any edit. This is the branch size where delegation could pay: the
# neutral prompt never mentions sub-agents, so an arm that does the work
# itself is only penalised by the clock.
python3 check.py
test -z "$(grep -rn 'fmt_amount([a-z_]*)' pkgs/ || true)"
