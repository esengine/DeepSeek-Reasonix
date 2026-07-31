set -e
cd workdir
python3 -c 'from greet.hello import greet; assert greet("World") == "Hello, World!"'
python3 -c 'import ast; ast.parse(open("tests/test_hello.py").read())'
assert README exists
grep -q "greet" README.md
test -f greet/__init__.py
test -f greet/hello.py
test -f tests/test_hello.py
grep -q "test_greet_normal" tests/test_hello.py
grep -q "test_greet_empty" tests/test_hello.py
echo OK
