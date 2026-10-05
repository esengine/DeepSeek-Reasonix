"""Load person records from a CSV file."""


def load_records(path):
    """Return the list of validated (name, age, email) records in `path`.

    Blank lines are skipped.  Raises ValueError on the first invalid record.
    """
    records = []
    with open(path, encoding="utf-8") as f:
        for lineno, line in enumerate(f, 1):
            line = line.strip()
            if not line:
                continue
            parts = line.split(",")
            if len(parts) != 3:
                raise ValueError(f"line {lineno}: expected 3 fields, got {len(parts)}")
            name, age, email = parts
            name = name.strip()
            if not name:
                raise ValueError(f"line {lineno}: name is empty")
            try:
                age = int(age)
            except ValueError:
                raise ValueError(f"line {lineno}: age must be an integer")
            if age < 0 or age > 150:
                raise ValueError(f"line {lineno}: age out of range")
            email = email.strip()
            if "@" not in email:
                raise ValueError(f"line {lineno}: invalid email")
            records.append((name, age, email))
    return records
