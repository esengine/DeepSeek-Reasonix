def load_text(path):
    """File contents as text; byte-order marks must not leak into the data."""
    with open(path, encoding="utf-8") as f:
        return f.read()
