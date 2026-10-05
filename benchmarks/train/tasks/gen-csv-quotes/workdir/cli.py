"""Command-line wrapper around the CSV reader.

Usage: python3 cli.py FILE
Prints each row as pipe-separated fields.
"""
import sys

from csv_reader import read_csv_file


def main(argv=None):
    argv = sys.argv[1:] if argv is None else argv
    if len(argv) != 1:
        print("usage: cli.py FILE", file=sys.stderr)
        return 2
    for row in read_csv_file(argv[0]):
        print("|".join(row))
    return 0


if __name__ == "__main__":
    sys.exit(main())
