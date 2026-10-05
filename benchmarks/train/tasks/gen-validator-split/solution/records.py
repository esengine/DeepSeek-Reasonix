"""Load person records from a CSV file.

Validation is separated from I/O: `validate_record` parses and validates a
single line without touching the filesystem, and `load_records` only reads
lines and delegates validation to it.
"""


def validate_record(line):
    """Parse and validate a single non-empty record line.

    Returns the (name, age, email) tuple.  Raises ValueError with a
    descriptive message when the line is malformed.  Performs no file I/O.
    """
    line = line.strip()
    parts = line.split(",")
    if len(parts) != 3:
        raise ValueError(f"expected 3 fields, got {len(parts)}")
    name, age, email = parts
    name = name.strip()
    if not name:
        raise ValueError("name is empty")
    try:
        age = int(age)
    except ValueError:
        raise ValueError("age must be an integer")
    if age < 0 or age > 150:
        raise ValueError("age out of range")
    email = email.strip()
    if "@" not in email:
        raise ValueError("invalid email")
    return (name, age, email)


def load_records(path):
    """Return the list of validated records from the file at `path`.

    Blank lines are skipped.  Raises ValueError on the first invalid record.
    """
    records = []
    with open(path, encoding="utf-8") as f:
        for lineno, line in enumerate(f, 1):
            line = line.strip()
            if not line:
                continue
            try:
                records.append(validate_record(line))
            except ValueError as exc:
                raise ValueError(f"line {lineno}: {exc}") from None
    return records
