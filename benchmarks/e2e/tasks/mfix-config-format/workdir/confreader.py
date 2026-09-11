def load(path):
    settings = {}
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            key, value = line.split(": ", 1)
            settings[key] = value
    return settings
