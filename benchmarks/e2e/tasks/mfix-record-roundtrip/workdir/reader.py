def load(path):
    """Read (name, score) records written by writer.dump."""
    records = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            name, score = line.split(",")
            records.append((name, int(score)))
    return records
