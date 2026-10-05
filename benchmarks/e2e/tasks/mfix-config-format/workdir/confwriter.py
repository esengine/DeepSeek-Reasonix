def dump(settings, path):
    with open(path, "w") as f:
        for key in sorted(settings):
            f.write(f"{key}={settings[key]}\n")
