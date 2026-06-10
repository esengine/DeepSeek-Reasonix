# Network and Proxy Gate

Use this reference when Earth Engine authentication, API requests, online dataset pages, or first-time model downloads may need external network access.

## When to Ask

Ask whether a proxy is needed before running auth or network-dependent code when the user is in mainland China or another restricted network.

Ask for:

- proxy type: HTTP, HTTPS, or SOCKS
- host: usually `127.0.0.1` for a local proxy
- port: for example `7890` or `1080`, depending on the user's proxy software
- whether the proxy should apply only to this terminal session

## Rules

- Prefer shell environment variables over hard-coded proxy settings in shared scripts.
- Never commit proxy credentials, private proxy URLs, tokens, cookies, or account information.
- Use placeholders in reusable code unless the user explicitly provides a safe local port.
- Record the decision in `RUN.md`: no proxy, system proxy, or explicit local `HOST:PORT`.

## PowerShell Example

```powershell
$env:HTTPS_PROXY = "http://127.0.0.1:7890"
$env:HTTP_PROXY = "http://127.0.0.1:7890"
# Use ALL_PROXY only when the user's local proxy is SOCKS-compatible.
$env:ALL_PROXY = "socks5://127.0.0.1:1080"
```

## Python Placeholder

Place optional proxy setup before the first network request:

```python
import os

# Optional: enable only if this machine needs a local proxy.
# os.environ["HTTPS_PROXY"] = "http://127.0.0.1:PORT"
# os.environ["HTTP_PROXY"] = "http://127.0.0.1:PORT"
```

Do not place real credentials in code examples.
