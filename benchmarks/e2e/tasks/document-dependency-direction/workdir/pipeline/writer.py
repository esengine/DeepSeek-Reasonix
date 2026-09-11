"""Writes normalized rows. Sits above the normalizer."""

from pipeline import normalizer


def write_normalized(src, dst):
    with open(dst, "w") as f:
        for row in normalizer.normalized(src):
            f.write(row + "\n")
