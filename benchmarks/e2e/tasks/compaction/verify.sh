set -e
got=$(tr -d '[:space:]' < answer.txt | tr '[:upper:]' '[:lower:]')
want="aldermoor-verrin"
[ "$got" = "$want" ] || { echo "answer.txt normalized to '$got', want '$want'"; exit 1; }
