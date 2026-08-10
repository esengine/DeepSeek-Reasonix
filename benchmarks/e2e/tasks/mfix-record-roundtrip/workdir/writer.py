def dump(records, path):
    """Write (name, score) records one per line."""
    with open(path, "w") as f:
        for name, score in records:
            f.write(f"{name}|{score}\n")
