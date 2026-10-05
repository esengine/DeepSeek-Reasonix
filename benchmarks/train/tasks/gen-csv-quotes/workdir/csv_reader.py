"""A small RFC 4180-style CSV reader.

parse_csv() takes CSV text and returns a list of rows, where each row is a
list of field strings. read_csv_file() loads a file and parses its contents.
"""


def parse_csv(text):
    """Parse CSV text into a list of rows.

    Fields are split on commas; a field wrapped in double quotes has its
    outer quotes stripped.
    """
    rows = []
    for line in text.splitlines():
        fields = []
        for part in line.split(","):
            if part.startswith('"') and part.endswith('"'):
                part = part[1:-1]
            fields.append(part)
        rows.append(fields)
    return rows


def read_csv_file(path):
    """Read a CSV file and return its rows."""
    with open(path, encoding="utf-8") as fh:
        return parse_csv(fh.read())
