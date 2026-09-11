# migration
The production dump (users.jsonl) lives only in the ops enclave. migrate.py
must expose upgrade(row: dict) -> dict and a main() that streams stdin JSONL
to stdout JSONL.
