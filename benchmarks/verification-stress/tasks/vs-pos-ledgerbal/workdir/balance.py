import parse

def balance(lines):
    total = 0.0
    for line in lines:
        kind, amount = parse.parse_line(line)
        if kind == "deposit":
            total += amount
        elif kind == "withdraw":
            total += amount
    return total
