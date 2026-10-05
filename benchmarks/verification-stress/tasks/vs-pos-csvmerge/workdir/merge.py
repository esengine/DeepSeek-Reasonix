import normalize

def merge_files(paths):
    rows = []
    header = None
    for p in paths:
        lines = [l.rstrip("\n") for l in open(p)]
        if not lines:
            continue
        if header is None:
            header = normalize.header(lines[0])
        rows.extend(normalize.row(l) for l in lines[2:])
    return [header] + rows
