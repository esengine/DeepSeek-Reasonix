#!/usr/bin/env python3
"""
Parallel Scheduler — 替代 subagent 的多Agent编排引擎

架构:
  Orchestrator Agent (reasonix run) → 决策：分配什么任务给谁
  Scheduler (这个脚本) → 解析指令 → 并行启动Worker → 收集结果
  Worker Agents (reasonix run × N) → 干活 → 输出结果

缓存保证: 每个Agent的system prompt固定不变，只有task变化
"""

import subprocess, json, os, sys, time, tempfile, hashlib
from pathlib import Path

REASONIX_BIN = os.environ.get("REASONIX_BIN", "npx reasonix")
MODEL = os.environ.get("REASONIX_MODEL", "deepseek-chat")
NO_CONFIG = ["--no-config"]

def reasonix_cmd() -> list:
    """返回 reasonix 命令行前缀 (list)"""
    # REASONIX_BIN 可以是 "npx reasonix" 或 "node /path/to/cli.js"
    return REASONIX_BIN.split()

# ============================================================
# 固定 System Prompts (缓存友好 — 绝不变化)
# ============================================================

ORCHESTRATOR_SYSTEM = """你是 Parallel 编排器。你协调多个 Worker Agent 完成复杂任务。

## 你的职责
1. 分析任务 → 拆解为可并行的子任务
2. 判断子任务目标是否重叠（重叠=必须串行，不重叠=可并行）
3. 每次输出 dispatch 指令分配可并行执行的子任务
4. Worker 完成后，根据结果决定下一步

## 输出格式
需要分配任务时:
{"action":"dispatch","workers":[{"id":"1","role":"角色","task":"具体任务描述"}]}

全部完成时:
{"action":"done","summary":"完成总结"}

## 可用角色
- architect: 架构设计、API设计、数据库Schema、技术栈选型
- security: 安全审查、认证授权、加密策略、审计日志
- performance: 性能优化、缓存策略、搜索方案、容量规划
- bugfixer: Bug诊断、代码修复、根因分析
- reviewer: 代码审查、质量检查、方案验证

## 规则
- 分析/设计类子任务 → 可并行（不同关注点，互不干扰）
- 修改代码类子任务 → 检查目标文件是否重叠
- 目标文件重叠 → 必须串行
- 一个 batch 尽量多分配可并行的子任务"""

ARCHITECT_SYSTEM = """你是后端架构师。专注: API设计、数据库Schema、技术栈选型、系统可扩展性。
规则: 不调用MCP工具。直接输出完整方案。输出末尾标注 [DONE]"""

SECURITY_SYSTEM = """你是安全专家。专注: 认证授权、注入防护、数据加密、审计日志。
规则: 不调用MCP工具。直接输出完整方案。输出末尾标注 [DONE]"""

PERFORMANCE_SYSTEM = """你是性能分析师。专注: 缓存策略、数据库优化、并发处理、搜索性能。
规则: 不调用MCP工具。直接输出完整方案。输出末尾标注 [DONE]"""

BUGFIXER_SYSTEM = """你是Bug修复专家。分析错误、定位根因、给出修复代码。
规则: 不调用MCP工具。直接输出分析和修复。输出末尾标注 [DONE]"""

REVIEWER_SYSTEM = """你是代码审查员。检查方案的正确性、完整性、可行性。
规则: 不调用MCP工具。指出问题和改进建议。输出末尾标注 [DONE]"""

WORKER_SYSTEMS = {
    "architect": ARCHITECT_SYSTEM,
    "security": SECURITY_SYSTEM,
    "performance": PERFORMANCE_SYSTEM,
    "bugfixer": BUGFIXER_SYSTEM,
    "reviewer": REVIEWER_SYSTEM,
}

# ============================================================
# 核心函数
# ============================================================

def run_agent(system_prompt: str, task: str, model: str = MODEL) -> str:
    """调用 reasonix run，返回 stdout 文本"""
    result = subprocess.run(
        reasonix_cmd() + ["run", task, "-m", model, "--system", system_prompt] + NO_CONFIG,
        capture_output=True, text=True, encoding="utf-8", timeout=300,
    )
    if result.returncode != 0:
        print(f"  [Agent 错误] exit={result.returncode}, stderr={result.stderr[:200]}", file=sys.stderr)
    return (result.stdout or "").strip()


def parse_cache(text: str) -> float:
    """从 agent 输出中提取缓存命中率"""
    import re
    m = re.search(r"cache:(\d+\.?\d*)%", text)
    return float(m.group(1)) if m else 0.0

def print_cache_summary(orch_caches: list, worker_caches: list):
    """输出缓存统计摘要"""
    print("\n=== 缓存统计 ===", file=sys.stderr)
    if orch_caches:
        avg_o = sum(orch_caches) / len(orch_caches)
        print(f"编排器: {len(orch_caches)}轮, 平均缓存 {avg_o:.1f}%, 首轮 {orch_caches[0]:.1f}%", file=sys.stderr)
    if worker_caches:
        avg_w = sum(worker_caches) / len(worker_caches)
        print(f"Worker: {len(worker_caches)}个, 平均缓存 {avg_w:.1f}%", file=sys.stderr)

def parse_orchestrator_output(text: str) -> dict:
    """从编排器输出中提取 JSON 指令"""
    # 找到最后一个 JSON 块
    text = text.strip()
    # 尝试找到 {...} JSON
    brace_start = text.rfind('{"action"')
    if brace_start == -1:
        # 没找到JSON，检查是否是纯文本DONE
        if "[DONE]" in text or "done" in text.lower():
            return {"action": "done", "summary": text}
        return {"action": "error", "message": "No JSON found", "raw": text[:500]}

    try:
        json_str = text[brace_start:]
        # 找到匹配的 }
        depth = 0
        end = brace_start
        for i, c in enumerate(text[brace_start:], brace_start):
            if c == '{':
                depth += 1
            elif c == '}':
                depth -= 1
                if depth == 0:
                    end = i + 1
                    break
        return json.loads(text[brace_start:end])
    except json.JSONDecodeError:
        return {"action": "error", "message": "JSON parse failed", "raw": text[-500:]}


def build_orchestrator_task(original_task: str, blackboard_state: dict,
                            round_num: int, worker_results: list = None) -> str:
    """构建编排器的 task 输入"""
    parts = [f"## 原始任务\n{original_task}\n"]

    if worker_results:
        parts.append("## 上一轮 Worker 结果")
        for wr in worker_results:
            parts.append(f"### {wr['role']} (id={wr['id']})")
            parts.append(wr['output'][:2000])
            parts.append("")

    parts.append(f"## 当前状态\n轮次: {round_num}")
    parts.append(f"已完成: {len(blackboard_state.get('completed',[]))} 个子任务")

    if blackboard_state.get("completed"):
        parts.append("已完成的任务:")
        for c in blackboard_state["completed"]:
            parts.append(f"  - {c}")

    parts.append("\n请决定下一步: dispatch 新的并行 batch, 或 done。")
    return "\n".join(parts)


def run_worker_batch(workers: list, blackboard_dir: str) -> list:
    """并行执行一批 Worker"""
    # 写入 worker 任务和 system prompt 到黑板
    worker_dir = os.path.join(blackboard_dir, "workers")
    os.makedirs(worker_dir, exist_ok=True)

    processes = {}
    for w in workers:
        wid = w["id"]
        role = w["role"]
        task = w["task"]

        system = WORKER_SYSTEMS.get(role, ARCHITECT_SYSTEM)
        out_file = os.path.join(worker_dir, f"{wid}_output.md")

        cmd = reasonix_cmd() + ["run", task, "-m", MODEL, "--system", system] + NO_CONFIG
        with open(out_file, "w", encoding="utf-8") as out:
            p = subprocess.Popen(cmd, stdout=out, stderr=subprocess.DEVNULL)
            processes[wid] = (p, out_file, role)

    # 等待全部完成
    results = []
    for wid, (p, out_file, role) in processes.items():
        p.wait()
        with open(out_file, encoding="utf-8") as f:
            output = f.read()
        results.append({"id": wid, "role": role, "output": output})
        print(f"  [Worker {wid} ({role}) 完成, {len(output)} 字符]", file=sys.stderr)

    return results


def run(task: str, max_rounds: int = 10) -> str:
    """主编排循环"""
    blackboard_dir = tempfile.mkdtemp(prefix="parallel_sched_")
    state = {"completed": [], "rounds": 0}

    print(f"Parallel Scheduler 启动", file=sys.stderr)
    print(f"黑板: {blackboard_dir}", file=sys.stderr)
    print(f"任务: {task[:100]}...", file=sys.stderr)

    all_results = []

    orch_caches = []
    worker_caches = []

    for round_num in range(1, max_rounds + 1):
        print(f"\n--- Round {round_num} ---", file=sys.stderr)

        # 1. 调用编排器
        orch_task = build_orchestrator_task(task, state, round_num, all_results[-3:] if all_results else None)
        orch_output = run_agent(ORCHESTRATOR_SYSTEM, orch_task)
        orch_cache = parse_cache(orch_output)
        orch_caches.append(orch_cache)
        print(f"[编排器] cache={orch_cache:.1f}% output={len(orch_output)}chars", file=sys.stderr)

        # 2. 解析指令
        instruction = parse_orchestrator_output(orch_output)

        # 3. 根据指令行动
        if instruction["action"] == "done":
            print(f"\n编排完成 — {round_num} 轮", file=sys.stderr)
            print_cache_summary(orch_caches, worker_caches)
            return instruction.get("summary", orch_output)

        elif instruction["action"] == "dispatch":
            workers = instruction.get("workers", [])
            print(f"Dispatch {len(workers)} 个 Worker (并行)", file=sys.stderr)

            # 4. 并行执行
            batch_results = run_worker_batch(workers, blackboard_dir)
            all_results.extend(batch_results)

            for br in batch_results:
                state["completed"].append(f"{br['role']}:{br['id']}")
                wcache = parse_cache(br["output"])
                worker_caches.append(wcache)
                print(f"  [Worker {br['role']}] cache={wcache:.1f}% {len(br['output'])}chars", file=sys.stderr)

            state["rounds"] = round_num

        else:
            print(f"编排器输出异常: {instruction.get('message','')}", file=sys.stderr)
            if round_num == 1:
                print("首轮编排失败，使用默认并行分析策略", file=sys.stderr)
                default_workers = [
                    {"id": "1", "role": "architect", "task": f"从架构角度分析: {task}"},
                    {"id": "2", "role": "security", "task": f"从安全角度分析: {task}"},
                    {"id": "3", "role": "performance", "task": f"从性能角度分析: {task}"},
                ]
                batch_results = run_worker_batch(default_workers, blackboard_dir)
                all_results.extend(batch_results)

    # 达到最大轮次
    return f"达到最大轮次 {max_rounds}。完成 {len(state['completed'])} 个子任务:\n" + \
           "\n".join(state["completed"])


# ============================================================
# CLI
# ============================================================

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: parallel-scheduler.py <task>", file=sys.stderr)
        sys.exit(1)

    task = sys.argv[1]
    start = time.time()
    result = run(task)
    elapsed = int(time.time() - start)

    print()
    print("=" * 60)
    print(result)
    print("=" * 60)
    print(f"总耗时: {elapsed}s", file=sys.stderr)
