from lib.textutil import normalize


def parse_row(line):
    return [field for field in normalize(line).split(";") if field]
