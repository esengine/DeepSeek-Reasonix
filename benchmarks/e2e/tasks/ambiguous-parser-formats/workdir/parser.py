import os


def parse_file(path):
    records = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            name, value = line.split(",")
            records.append((name.strip(), int(value)))
    return records


def parse_all(directory):
    """All (name, value) records across every data file in directory, sorted."""
    records = []
    for entry in sorted(os.listdir(directory)):
        records.extend(parse_file(os.path.join(directory, entry)))
    return sorted(records)
