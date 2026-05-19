"""Parallel MCP Server — 多Agent并行执行引擎

启动: parallel-mcp --stdio  (或 python parallel_server.py --stdio)

独立包，无外部依赖（仅需 mcp>=1.0.0，Reasonix 用户已自带）。
"""

import json
import os
import re
import sys
import uuid
import time
import tempfile
import subprocess
import threading

from mcp.server import Server
from mcp.server.stdio import stdio_server
from mcp.types import Tool, TextContent

server = Server("parallel-v1.0.0")

_parallel_tasks: dict = {}
_parallel_lock = threading.Lock()

WORKER_SYSTEMS = {
    "architect": "你是后端架构师。专注: API设计、数据库Schema、技术栈选型、系统可扩展性。规则: 不调用MCP工具。直接输出完整方案。输出末尾标注 [DONE]",
    "security": "你是安全专家。专注: 认证授权、注入防护、数据加密、审计日志。规则: 不调用MCP工具。直接输出完整方案。输出末尾标注 [DONE]",
    "performance": "你是性能分析师。专注: 缓存策略、数据库优化、并发处理、搜索性能。规则: 不调用MCP工具。直接输出完整方案。输出末尾标注 [DONE]",
    "coder": "你是软件工程师。用filesystem工具直接读写文件、修改代码。完成后读取文件验证修改正确。不调用其他MCP。输出末尾标注 [DONE]",
}


def _find_reasonix_js():
    candidates = [
        "D:/Reasonix/dist/cli/index.js",
        "/d/Reasonix/dist/cli/index.js",
    ]
    import glob as _glob
    for prefix in [os.environ.get("APPDATA", ""), os.environ.get("HOME", ""),
                   "/c/Users", "C:/Users"]:
        for p in _glob.glob(os.path.join(prefix, "*", "node_modules", "reasonix", "dist", "cli", "index.js")):
            candidates.append(p)
    # npm global install
    for prefix in ["/c/Users", "C:/Users"]:
        for p in _glob.glob(os.path.join(prefix, "*", "node_modules", "reasonix", "dist", "cli", "index.js")):
            candidates.append(p)
    for c in candidates:
        if os.path.isfile(c):
            return c
    # mac/linux
    import shutil
    found = shutil.which("reasonix")
    if found:
        return found
    return None


def _run_parallel_schedule(task, mode, workdir, model, tasks_list=None):
    rx_js = _find_reasonix_js()
    if not rx_js:
        return {"error": "reasonix CLI not found"}

    no_config = ["--no-config"]
    start_time = time.time()
    tasks_list = tasks_list or []

    if mode == "code":
        if not workdir:
            return {"error": "code模式需要 workdir"}
        if tasks_list and len(tasks_list) > 1:
            workers = []
            for i, t in enumerate(tasks_list, 1):
                workers.append({
                    "id": str(i), "role": f"coder_{i}",
                    "task": f"{t}\n\n用filesystem工具直接修改代码文件。只改你负责的文件，不要碰其他文件。完成后读取文件验证修改正确。",
                    "system": WORKER_SYSTEMS["coder"],
                    "extra_args": ["--mcp", f"fs=npx -y @modelcontextprotocol/server-filesystem {workdir}"],
                })
        else:
            workers = [{
                "id": "1", "role": "coder",
                "task": f"{task}\n\n用filesystem工具直接修改代码文件。完成后读取文件验证修改正确。",
                "system": WORKER_SYSTEMS["coder"],
                "extra_args": ["--mcp", f"fs=npx -y @modelcontextprotocol/server-filesystem {workdir}"],
            }]
    else:
        workers = [
            {"id": "1", "role": "architect", "task": f"从架构角度设计方案: {task}",
             "system": WORKER_SYSTEMS["architect"], "extra_args": []},
            {"id": "2", "role": "security", "task": f"从安全角度设计方案: {task}",
             "system": WORKER_SYSTEMS["security"], "extra_args": []},
            {"id": "3", "role": "performance", "task": f"从性能角度设计方案: {task}",
             "system": WORKER_SYSTEMS["performance"], "extra_args": []},
        ]

    tmpdir = tempfile.mkdtemp(prefix="parallel_")
    procs = {}
    for w in workers:
        cmd = ["node", rx_js, "run", w["task"], "-m", model, "--system", w["system"]] + no_config + w["extra_args"]
        out_path = os.path.join(tmpdir, f"worker_{w['id']}.txt")
        with open(out_path, "w", encoding="utf-8") as out:
            startupinfo = None
            if sys.platform == "win32":
                startupinfo = subprocess.STARTUPINFO()
                startupinfo.dwFlags |= subprocess.STARTF_USESHOWWINDOW
                startupinfo.wShowWindow = subprocess.SW_HIDE
            p = subprocess.Popen(cmd, stdout=out, stderr=subprocess.DEVNULL,
                                 startupinfo=startupinfo,
                                 creationflags=subprocess.CREATE_NO_WINDOW if sys.platform == "win32" else 0)
            procs[w["id"]] = (p, out_path, w["role"])

    results = {}
    cache_hits = {}
    for wid, (p, out_path, role) in procs.items():
        p.wait()
        with open(out_path, "r", encoding="utf-8", errors="replace") as f:
            output = f.read()
        m = re.search(r"cache:(\d+\.?\d*)%", output)
        cache_hits[role] = float(m.group(1)) if m else 0.0
        results[role] = {
            "role": role,
            "length": len(output),
            "cache_pct": cache_hits[role],
            "content": output[:3000],
        }

    elapsed = int(time.time() - start_time)
    summary = {
        "mode": mode,
        "workers": len(workers),
        "elapsed_s": elapsed,
        "cache_hits": cache_hits,
        "avg_cache_pct": round(sum(cache_hits.values()) / len(cache_hits), 1) if cache_hits else 0,
        "total_chars": sum(r["length"] for r in results.values()),
        "results": results,
    }
    if mode == "code":
        summary["combined"] = results.get("coder", {}).get("content", "") if len(results) == 1 else "\n\n".join(
            f"## {r['role']}\n{r['content']}" for r in results.values())
    else:
        summary["combined"] = "\n\n---\n\n".join(
            f"## {role}\n{results[role]['content']}" for role in ["architect", "security", "performance"] if role in results)
    return summary


def _run_parallel_async(task_id, task, mode, workdir, model, tasks_list=None):
    try:
        result = _run_parallel_schedule(task, mode, workdir, model, tasks_list)
        with _parallel_lock:
            _parallel_tasks[task_id] = {**result, "status": "completed", "task_id": task_id}
    except Exception as e:
        with _parallel_lock:
            _parallel_tasks[task_id] = {"status": "error", "error": str(e), "task_id": task_id}


@server.list_tools()
async def list_tools():
    return [
        Tool(
            name="parallel_schedule",
            description="多Agent并行引擎。3个独立Worker并行设计方案或执行编程任务。替代subagent，固定system prompt ~90%缓存。mode=design(3Agent并行设计)/code(编程执行,带文件系统MCP)。",
            inputSchema={
                "type": "object",
                "properties": {
                    "task": {"type": "string", "description": "任务描述"},
                    "tasks": {
                        "type": "array", "items": {"type": "string"},
                        "description": "code模式多任务列表，每个产生一个Worker。文件不能重叠。",
                        "default": [],
                    },
                    "mode": {
                        "type": "string", "enum": ["design", "code"], "default": "design",
                        "description": "design=3Agent并行设计方案 / code=执行编程任务",
                    },
                    "workdir": {"type": "string", "default": "", "description": "code模式的代码目录(Windows绝对路径)"},
                    "model": {"type": "string", "default": "deepseek-chat", "description": "模型ID"},
                },
                "required": ["task"],
            },
        ),
        Tool(
            name="parallel_status",
            description="查询 parallel_schedule 任务进度和结果。",
            inputSchema={
                "type": "object",
                "properties": {
                    "task_id": {"type": "string", "description": "parallel_schedule 返回的任务ID"},
                    "seq": {"type": "integer", "description": "轮询序号(1,2,3...递增)，避免重复调用拦截"},
                },
                "required": ["task_id"],
            },
        ),
    ]


@server.call_tool()
async def call_tool(name: str, arguments: dict):
    if name == "parallel_schedule":
        task = arguments["task"]
        tasks_list = arguments.get("tasks", [])
        mode = arguments.get("mode", "design")
        workdir = arguments.get("workdir", "")
        model = arguments.get("model", "deepseek-chat")
        task_id = f"ps_{uuid.uuid4().hex[:8]}"
        with _parallel_lock:
            _parallel_tasks[task_id] = {"status": "running", "mode": mode, "task": task[:80]}
        t = threading.Thread(target=_run_parallel_async, args=(task_id, task, mode, workdir, model, tasks_list))
        t.daemon = True
        t.start()
        workers = 3 if mode == "design" else (max(1, len(tasks_list)) if tasks_list else 1)
        return [TextContent(type="text", text=json.dumps({
            "task_id": task_id, "status": "started", "mode": mode, "workers": workers,
            "note": f"Parallel {mode} 模式已启动 ({workers} Worker)。用 parallel_status(task_id=\"{task_id}\", seq=1) 查询进度。",
        }, ensure_ascii=False, indent=2))]

    if name == "parallel_status":
        task_id = arguments["task_id"]
        with _parallel_lock:
            info = _parallel_tasks.get(task_id)
        if not info:
            return [TextContent(type="text", text=json.dumps({"error": f"任务不存在: {task_id}"}, ensure_ascii=False))]
        info["task_id"] = task_id
        return [TextContent(type="text", text=json.dumps(info, ensure_ascii=False, indent=2))]

    return [TextContent(type="text", text=f"Unknown tool: {name}")]


def main():
    import asyncio
    asyncio.run(_main())


async def _main():
    async with stdio_server() as (read, write):
        await server.run(read, write, server.create_initialization_options())


if __name__ == "__main__":
    main()
