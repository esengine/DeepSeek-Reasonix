# eventlog

Append-only event log with sequence numbers.

```python
from eventlog import Log

log = Log("events.log")
log.append(kind, name)  # writes "<seq> <kind> <name>" with seq starting at 1
log.count()             # entries appended through this Log instance
```
