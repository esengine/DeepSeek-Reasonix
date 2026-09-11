import sys

from lib.rows import parse_row
from lib.tally import tally


def main(path):
    with open(path) as f:
        rows = [parse_row(line) for line in f if line.strip()]
    return tally(rows)


if __name__ == "__main__":
    print(main(sys.argv[1]))
