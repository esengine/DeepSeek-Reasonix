set -e
# Three defects in three non-overlapping directories. A single agent must fix
# them serially; parallel writers with disjoint write_paths are the shape this
# task exists to price. The prompt never mentions delegation, so an arm that
# does not delegate is not penalised by the rules, only by the clock.
python3 check.py
