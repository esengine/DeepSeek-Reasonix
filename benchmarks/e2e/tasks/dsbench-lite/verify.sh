set -e
cd workdir
python3 -m pytest test_app.py -q 2>/dev/null || python3 test_app.py
echo OK
