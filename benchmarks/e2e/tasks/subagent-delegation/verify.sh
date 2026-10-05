set -e
# Exercises a fresh `task` sub-agent delegation in the headless `reasonix run`
# path: 17 + 28 + 41 = 86. The numbers are arbitrary so the answer can only be
# produced by actually reading the three seed files (the prompt mandates doing
# that via the `task` tool). Before sub-agents could run without a parent
# session, the `task` call errored here with "parent session is required".
test -f result.txt
got=$(tr -d '[:space:]' < result.txt)
if [ "$got" != "86" ]; then
  echo "result.txt = '$got', want 86"
  exit 1
fi
