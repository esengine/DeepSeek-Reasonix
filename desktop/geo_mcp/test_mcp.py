"""
MCP Server 冒烟测试 — 通过子进程逐个端点发送 JSON-RPC，验证响应。

用法:
    conda run -n gee python internal/geo/mcp_server/test_mcp.py
"""

import subprocess
import json
import sys
import os

# conda run 可能用 GBK 编码 stdout，强制 UTF-8 避免 UnicodeEncodeError
if sys.platform == "win32":
    import io
    sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")

MCP_MODULE = "internal.geo.mcp_server"

PASS = "PASS"
FAIL = "FAIL"

_next_id = 1


def _next_req_id() -> int:
    global _next_id
    rid = _next_id
    _next_id += 1
    return rid


def _send(proc: subprocess.Popen, req: dict) -> dict:
    """Send one JSON-RPC request, read one response line."""
    line = json.dumps(req, ensure_ascii=False) + "\n"
    proc.stdin.write(line)
    proc.stdin.flush()
    raw = proc.stdout.readline()
    if not raw:
        raise RuntimeError("no response from MCP server")
    return json.loads(raw)


def test_initialize(proc: subprocess.Popen) -> bool:
    resp = _send(proc, {"jsonrpc": "2.0", "id": _next_req_id(), "method": "initialize", "params": {}})
    ok = (
        resp.get("result", {}).get("protocolVersion") == "2024-11-05"
        and "tools" in resp.get("result", {}).get("capabilities", {})
    )
    print(f"[{PASS if ok else FAIL}] initialize  {resp.get('result', {}).get('serverInfo', {})}")
    return ok


def test_notification(proc: subprocess.Popen) -> bool:
    """Ensure notification (no id) doesn't crash the server."""
    proc.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}) + "\n")
    proc.stdin.flush()
    print(f"[{PASS}] notification  (silently ignored)")
    return True


def test_tools_list(proc: subprocess.Popen) -> bool:
    resp = _send(proc, {"jsonrpc": "2.0", "id": _next_req_id(), "method": "tools/list", "params": {}})
    tools = resp.get("result", {}).get("tools", [])
    ok = len(tools) == 5
    print(f"[{PASS if ok else FAIL}] tools/list  ({len(tools)} tools)")
    for t in tools:
        print(f"    - {t['name']}")
    return ok


def test_tool_call(proc: subprocess.Popen, name: str, args: dict, expect_error: bool = False) -> bool:
    resp = _send(proc, {
        "jsonrpc": "2.0", "id": _next_req_id(), "method": "tools/call",
        "params": {"name": name, "arguments": args},
    })
    content = resp.get("result", {}).get("content", [])
    is_error = resp.get("result", {}).get("isError", False)
    has_text = len(content) > 0 and content[0].get("type") == "text"

    ok = has_text and is_error == expect_error
    text_preview = content[0].get("text", "")[:120] if has_text else "<no text>"
    print(f"[{PASS if ok else FAIL}] tools/call {name}  isError={is_error}  {text_preview}")
    return ok


def test_unknown_method(proc: subprocess.Popen) -> bool:
    resp = _send(proc, {"jsonrpc": "2.0", "id": _next_req_id(), "method": "nonexistent", "params": {}})
    ok = resp.get("error", {}).get("code") == -32601
    print(f"[{PASS if ok else FAIL}] unknown_method  error_code={resp.get('error', {}).get('code')}")
    return ok


def main():
    script_dir = os.path.dirname(os.path.abspath(__file__))
    project_root = os.path.dirname(os.path.dirname(os.path.dirname(script_dir)))

    cmd = [sys.executable, "-m", MCP_MODULE]
    proc = subprocess.Popen(
        cmd,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        cwd=project_root,
    )

    results = []
    try:
        # 基础协议
        results.append(test_initialize(proc))
        results.append(test_notification(proc))
        results.append(test_tools_list(proc))

        # geo_env_status — 真实环境探测
        print("\n--- geo_env_status ---")
        results.append(test_tool_call(proc, "geo_env_status", {}))

        # read_geo_data — 无效路径应返回 isError
        results.append(test_tool_call(proc, "read_geo_data", {"path": "/nonexistent/file.tif"}, expect_error=True))

        # run_qgis_algorithm — 空参数 → QGIS 报错（isError=True）
        results.append(test_tool_call(proc, "run_qgis_algorithm", {"algorithm": "native:buffer", "params": {}}, expect_error=True))

        # run_gee_script — GEE ready，简单脚本应成功
        results.append(test_tool_call(proc, "run_gee_script", {"script": "print('hello')"}))

        # qgis_doc — 新参数格式
        results.append(test_tool_call(proc, "qgis_doc", {"action": "search", "query": "buffer"}))

        # 未知方法
        results.append(test_unknown_method(proc))
    finally:
        proc.stdin.close()
        proc.wait(timeout=5)

    passed = sum(results)
    total = len(results)
    print(f"\n{'='*40}")
    print(f"{passed}/{total} passed")
    if passed < total:
        stderr = proc.stderr.read()
        if stderr:
            print(f"\nstderr:\n{stderr[:2000]}")
    return 0 if passed == total else 1


if __name__ == "__main__":
    sys.exit(main())
