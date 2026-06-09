"""
JSON-RPC 2.0 MCP stdio server — read-dispatch-reply loop.

Protocol:
  - stdin  → read lines (JSON delimited by \\n)
  - stdout → write lines (JSON + \\n)  — reserved for JSON-RPC, no stray output
  - stderr → logs

Reference: cmd/reasonix-plugin-example/main.go
"""

import sys
import json
import logging
from typing import Any

from .geo_tools import tool_list, call_tool

logger = logging.getLogger(__name__)

PROTOCOL_VERSION = "2024-11-05"
SERVER_NAME = "rs-reasonix-geocode"
SERVER_VERSION = "0.1.0"

ERR_METHOD_NOT_FOUND = -32601
ERR_INVALID_PARAMS = -32602
ERR_INTERNAL = -32603


def serve() -> None:
    """Run the stdio read-dispatch-reply loop until stdin closes."""
    logging.basicConfig(
        stream=sys.stderr,
        level=logging.INFO,
        format="%(name)s: %(message)s",
    )
    logger.info("starting %s v%s", SERVER_NAME, SERVER_VERSION)

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError:
            logger.warning("skipping unparseable line: %s", line[:200])
            continue

        # notification — no reply
        if req.get("id") is None:
            continue

        resp = _dispatch(req)
        sys.stdout.write(json.dumps(resp, ensure_ascii=False) + "\n")
        sys.stdout.flush()


def _dispatch(req: dict) -> dict:
    method = req.get("method", "")
    rid = req.get("id")

    if method == "initialize":
        return _ok(rid, {
            "protocolVersion": PROTOCOL_VERSION,
            "capabilities": {"tools": {}},
            "serverInfo": {
                "name": SERVER_NAME,
                "version": SERVER_VERSION,
            },
        })

    if method == "tools/list":
        return _ok(rid, {"tools": tool_list()})

    if method == "tools/call":
        return _handle_tool_call(rid, req.get("params", {}))

    return _err(rid, ERR_METHOD_NOT_FOUND, f"method not found: {method}")


def _handle_tool_call(rid: Any, params: dict) -> dict:
    name = params.get("name")
    arguments = params.get("arguments", {})
    if not name:
        return _err(rid, ERR_INVALID_PARAMS, "missing tool name")

    try:
        text, is_error = call_tool(name, arguments)
    except Exception as exc:
        logger.exception("tool %s crashed", name)
        return _err(rid, ERR_INTERNAL, str(exc))

    return _ok(rid, {
        "content": [{"type": "text", "text": text}],
        "isError": is_error,
    })


def _ok(rid: Any, result: Any) -> dict:
    return {"jsonrpc": "2.0", "id": rid, "result": result}


def _err(rid: Any, code: int, message: str) -> dict:
    return {"jsonrpc": "2.0", "id": rid, "error": {"code": code, "message": message}}
