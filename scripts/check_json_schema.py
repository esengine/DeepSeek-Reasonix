#!/usr/bin/env python3
"""Validate a JSON config file against field type/length/enum constraints.
Usage: python check_json_schema.py <file> [--strings a,b] [--ints x,y] [--enums k=v,...] [--required a,b] [--maxlen k=v,...]
"""
import json, sys, os

def parse_kv(items):
    d = {}
    for item in items:
        if "=" in item:
            k, v = item.split("=", 1)
            d[k] = v
    return d

def parse_kvs(items):
    d = {}
    for item in items:
        if "=" in item:
            k, v = item.split("=", 1)
            d[k] = v.split(",")
    return d

def main():
    args = sys.argv[1:]
    if not args or args[0] in ("-h", "--help"):
        print(f"Usage: {sys.argv[0]} <file.json> [--strings a,b] [--ints x,y] [--enums k=v,...] [--required a,b] [--maxlen k=v,...]")
        sys.exit(0)

    filepath = args[0]
    if not os.path.exists(filepath):
        print(f"FAILED: file not found: {filepath}")
        sys.exit(1)

    try:
        with open(filepath, "r", encoding="utf-8") as f:
            data = json.load(f)
    except json.JSONDecodeError as e:
        print(f"INVALID JSON: {e}")
        sys.exit(2)
    except FileNotFoundError:
        print(f"MISSING: file not found: {filepath}")
        sys.exit(1)

    string_fields = []
    int_fields = []
    enum_fields = {}
    required_fields = []
    maxlen_fields = {}

    i = 1
    while i < len(args):
        if args[i] == "--strings" and i+1 < len(args):
            string_fields = [x.strip() for x in args[i+1].split(",")]
            i += 2
        elif args[i] == "--ints" and i+1 < len(args):
            int_fields = [x.strip() for x in args[i+1].split(",")]
            i += 2
        elif args[i] == "--enums" and i+1 < len(args):
            enum_fields = parse_kvs(args[i+1].split(","))
            i += 2
        elif args[i] == "--required" and i+1 < len(args):
            required_fields = [x.strip() for x in args[i+1].split(",")]
            i += 2
        elif args[i] == "--maxlen" and i+1 < len(args):
            maxlen_fields = parse_kv(args[i+1].split(","))
            i += 2
        else:
            i += 1

    errors = []

    for field in required_fields:
        if field not in data:
            errors.append(f"MISSING required field: {field}")

    for field in string_fields:
        val = data.get(field)
        if val is not None and not isinstance(val, str):
            errors.append(f"TYPE: {field} must be string (got {type(val).__name__})")

    for field in int_fields:
        val = data.get(field)
        if val is not None and not isinstance(val, int):
            errors.append(f"TYPE: {field} must be int (got {type(val).__name__})")

    for field, allowed in enum_fields.items():
        val = data.get(field)
        if val is not None and val not in allowed:
            errors.append(f"VALUE: {field}={val!r} must be one of {allowed}")

    for field, maxlen_str in maxlen_fields.items():
        val = data.get(field)
        if val is not None and isinstance(val, str) and len(val) > int(maxlen_str):
            errors.append(f"LENGTH: {field} exceeds {maxlen_str} chars (got {len(val)})")

    if errors:
        print(f"FAILED: {len(errors)} violation(s)\n")
        for e in errors:
            print(f"  {e}")
        sys.exit(1)
    else:
        print("OK: all schema checks passed")


if __name__ == "__main__":
    main()
