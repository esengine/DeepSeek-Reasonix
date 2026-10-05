# csv-reader

A tiny RFC 4180-style CSV reader.

- `csv_reader.py` — `parse_csv(text)` and `read_csv_file(path)`
- `cli.py` — command-line wrapper: `python3 cli.py FILE`
- `tests/` — behavioural tests: `python3 -m unittest discover -s tests`

Reported issue: quoted fields that contain the separator are not parsed
correctly.
