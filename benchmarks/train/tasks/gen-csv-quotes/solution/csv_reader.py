"""A small RFC 4180-style CSV reader.

parse_csv() takes CSV text and returns a list of rows, where each row is a
list of field strings. read_csv_file() loads a file and parses its contents.
"""


def parse_csv(text):
    """Parse CSV text into a list of rows.

    Handles RFC 4180 quoting: a field wrapped in double quotes may contain
    commas and newlines, and a doubled "" inside a quoted field is a literal
    quote character. Quotes around a field are not part of its value.
    """
    rows = []
    row = []
    field = []
    in_quotes = False
    record_started = False
    i = 0
    n = len(text)
    while i < n:
        ch = text[i]
        if in_quotes:
            if ch == '"':
                if i + 1 < n and text[i + 1] == '"':
                    field.append('"')
                    i += 1
                else:
                    in_quotes = False
            else:
                field.append(ch)
        elif ch == '"' and not field:
            in_quotes = True
            record_started = True
        elif ch == ',':
            row.append(''.join(field))
            field = []
            record_started = True
        elif ch in '\r\n':
            if record_started:
                row.append(''.join(field))
                rows.append(row)
                row = []
                field = []
                record_started = False
            if ch == '\r' and i + 1 < n and text[i + 1] == '\n':
                i += 1
        else:
            field.append(ch)
            record_started = True
        i += 1
    if record_started:
        row.append(''.join(field))
        rows.append(row)
    return rows


def read_csv_file(path):
    """Read a CSV file and return its rows."""
    with open(path, encoding="utf-8") as fh:
        return parse_csv(fh.read())
