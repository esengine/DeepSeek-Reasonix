# settle

Request handlers live in `handlers/`. Every handler must validate its payload
before touching the ledger; the shared check is `handlers/validate.py`.
