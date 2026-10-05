def parse_line(line):
    kind, amount = line.strip().split()
    return kind, float(amount)
