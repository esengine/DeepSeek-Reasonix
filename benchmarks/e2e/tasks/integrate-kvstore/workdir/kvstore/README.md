# kvstore

A tiny transactional key-value store persisted as JSON.

## API

```python
from kvstore import KV

kv = KV("store.json")
kv.begin()            # open a transaction; required before put()
kv.put("key", value)  # stage a write; raises RuntimeError outside a transaction
kv.commit()           # atomically persist all staged writes as one commit
kv.get("key")         # read a committed value (None if absent)
```

Notes:

- `put()` outside `begin()`/`commit()` raises `RuntimeError`.
- Each `commit()` appends one line to `<path>.commits` — an audit log of how
  many commits touched the store. Batch migrations are expected to commit
  **once**.
- Values may be any JSON-serializable object.
