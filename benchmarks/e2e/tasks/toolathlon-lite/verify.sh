set -e
test -f output/sum.txt
SUM=$(cat output/sum.txt | tr -d ' \n')
test "$SUM" = "30" && echo "sum ok"
test -f output/note.md
grep -q "Sum computed: 30" output/note.md
echo OK
