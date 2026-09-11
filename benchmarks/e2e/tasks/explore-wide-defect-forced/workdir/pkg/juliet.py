def normalise(label):
    """Trim whitespace and drop a trailing colon."""
    label = label.strip()
    if label.endswith(":"):
        label = label[:-1]
    return label
