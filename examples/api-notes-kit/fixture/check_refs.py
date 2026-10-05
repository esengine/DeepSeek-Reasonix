"""Check local JSON reference targets without fetching external material."""

import json
import sys
from pathlib import Path


def resolve(document, pointer):
    if pointer == "#":
        return document
    if not pointer.startswith("#/"):
        raise ValueError("only local JSON Pointer references are supported")
    node = document
    for token in pointer[2:].split("/"):
        if "~" in token.replace("~1", "").replace("~0", ""):
            raise ValueError("invalid JSON Pointer escape")
        key = token.replace("~1", "/").replace("~0", "~")
        if isinstance(node, list):
            if not key.isascii() or not key.isdigit() or (len(key) > 1 and key.startswith("0")):
                raise ValueError("invalid array index")
            node = node[int(key)]
        else:
            node = node[key]
    return node


def check(document, node, pointer="#"):
    failures = []
    if isinstance(node, dict):
        if "$ref" in node:
            target = node["$ref"]
            try:
                if not isinstance(target, str):
                    raise ValueError("a reference must be a string")
                resolve(document, target)
            except (KeyError, IndexError, TypeError, ValueError) as error:
                failures.append((pointer + "/$ref", target, str(error)))
        entries = node.items()
    elif isinstance(node, list):
        entries = enumerate(node)
    else:
        return failures
    for key, value in entries:
        token = str(key).replace("~", "~0").replace("/", "~1")
        failures.extend(check(document, value, pointer + "/" + token))
    return failures


def main():
    if len(sys.argv) != 2:
        print("Usage: python3 check_refs.py SOURCE.json", file=sys.stderr)
        return 2
    try:
        document = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
    except (OSError, ValueError) as error:
        print(error, file=sys.stderr)
        return 2
    failures = check(document, document)
    for pointer, target, error in failures:
        print(f"{pointer}: {target!r}: {error}", file=sys.stderr)
    if failures:
        return 1
    print("Local reference targets resolve. Schema, prose, and live API behavior are not checked.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
