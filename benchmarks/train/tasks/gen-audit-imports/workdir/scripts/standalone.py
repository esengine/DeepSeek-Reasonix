#!/usr/bin/env python3
"""Standalone runner script."""

import sys

from app.main import run


def main() -> int:
    return 0 if run() else 1


if __name__ == "__main__":
    sys.exit(main())
