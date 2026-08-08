import sys


def mean(path):
    with open(path) as f:
        values = [float(line) for line in f]
    return sum(values) / len(values)


if __name__ == "__main__":
    print(f"mean={mean(sys.argv[1])}")
