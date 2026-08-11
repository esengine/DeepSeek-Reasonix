def header(line):
    return ";".join(part.strip().lower() for part in line.split(","))

def row(line):
    return ",".join(part.strip() for part in line.split(","))
