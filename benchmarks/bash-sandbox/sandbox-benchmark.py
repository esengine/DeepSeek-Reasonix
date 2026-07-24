#!/usr/bin/env -S uv run
# /// script
# requires-python = ">=3.12"
# dependencies = [
#   "openai>=1.0.0",
#   "python-dotenv>=1.0.0",
# ]
# ///

"""
Step 1 Bash Sandbox Benchmark — evaluate how the sandbox_capabilities parameter
description affects LLM behavior in sandbox-restricted scenarios.

Seams:
  config     — scenario & diagnostic rule definitions
  variant    — build (sys_prompt, tools) from baseline
  baseline   — capture system prompt + tool schemas
  diagnostic — match error text → capability suggestion
  simulator  — build simulated tool responses
  llm        — communication with the API
  eval       — evaluation strategies (single-turn / multi-turn)
  report     — summary tables + JSON output

Usage:
    export DEEPSEEK_API_KEY="sk-..."
    uv run benchmarks/bash-sandbox/sandbox-benchmark.py

    # Optional: customize model, variants, scenarios
    uv run benchmarks/bash-sandbox/sandbox-benchmark.py \
        --model deepseek-chat \
        --base-url https://api.deepseek.com/v1 \
        --variants step1 step2 \
        --quick --trials 10
"""

import argparse
import copy
import json
import os
import subprocess
import sys
import tempfile
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any


# ═══════════════════════════════════════════════════════════════════
# Constants
# ═══════════════════════════════════════════════════════════════════

REPO_ROOT = Path(__file__).resolve().parents[2]
CAPTURE_CMD = ["go", "run", "./cmd/capture-baseline/"]

ORIGINAL_SANDBOX_DESC = (
    "Optional request for one atomic OS-sandbox capability delta. "
    "The host may run the command in the original sandbox instead."
)

NEW_SANDBOX_DESC = """Optional request for one-time OS-sandbox capability delta.
When bash exits non-zero, retry the SAME command with sandbox_capabilities matching:
"NVIDIA driver"→{"devices": [{"path":"/dev/nvidia0"},...]};
"unable to open database"/"Read-only file system"/"只读文件系统"/"Permission denied"(cache dirs)/"no write access to ... directory"→{"write_paths": [{"identity": "canonical_absolute","path":...},...]}
"Connection timed out" / resolution  → {"network": true}"""


# ═══════════════════════════════════════════════════════════════════
# Config Module — scenario & diagnostic rule definitions
# ═══════════════════════════════════════════════════════════════════


@dataclass
class SandboxScenario:
    """A benchmark scenario: prompt, expected capability, sandbox error text."""

    id: str
    prompt_zh: str
    prompt_en: str
    expected_cap: str  # "write_paths" | "devices" | "network"
    sandbox_error: str
    sandbox_exit_code: int
    bash_command: str  # the command that triggers the sandbox error
    suggested_cap: dict | None = None

    def prompt(self, lang: str) -> str:
        return self.prompt_zh if lang == "zh" else self.prompt_en


SCENARIOS = [
    SandboxScenario(
        id="pnpm-install",
        prompt_zh="用 pnpm 安装 prettier",
        prompt_en="Install prettier with pnpm",
        expected_cap="write_paths",
        sandbox_error="[ERR_SQLITE_ERROR] unable to open database file\npnpm: unable to open database file",
        sandbox_exit_code=1,
        bash_command="pnpm install prettier",
    ),
    SandboxScenario(
        id="pnpm-global",
        prompt_zh="用 pnpm 全局安装 prettier（pnpm install -g prettier）",
        prompt_en="Install prettier globally with pnpm install -g",
        expected_cap="write_paths",
        sandbox_error="[ERROR] The CLI has no write access to the global bin directory at "
        + os.path.expanduser("~/.local/share/pnpm/bin"),
        sandbox_exit_code=1,
        bash_command="pnpm install -g prettier",
    ),
    SandboxScenario(
        id="gpu-info",
        prompt_zh="查看 NVIDIA GPU 的详细信息（型号、显存、驱动版本）",
        prompt_en="Show detailed NVIDIA GPU info (model, VRAM, driver version)",
        expected_cap="devices",
        sandbox_error="couldn't communicate with the NVIDIA driver",
        sandbox_exit_code=9,
        bash_command="nvidia-smi",
        suggested_cap={
            "devices": [
                {"path": "/dev/nvidia0"},
                {"path": "/dev/nvidiactl"},
                {"path": "/dev/nvidia-uvm"},
            ]
        },
    ),
]


@dataclass
class DiagnosticRule:
    """A keyword → capability mapping for sandbox error diagnostics."""

    keyword: str
    cap_type: str
    suggested_cap: dict | None
    description: str


DIAG_RULES = [
    DiagnosticRule(
        "couldn't communicate with the NVIDIA driver",
        "devices",
        {"devices": [{"path": "/dev/nvidia0"}, {"path": "/dev/nvidiactl"}, {"path": "/dev/nvidia-uvm"}]},
        "NVIDIA driver access",
    ),
    DiagnosticRule(
        "NVIDIA-SMI has failed",
        "devices",
        {"devices": [{"path": "/dev/nvidia0"}, {"path": "/dev/nvidiactl"}, {"path": "/dev/nvidia-uvm"}]},
        "NVIDIA driver access",
    ),
    DiagnosticRule("unable to open database", "write_paths", None, "write to package manager store"),
    DiagnosticRule("Read-only file system", "write_paths", None, "write to read-only filesystem"),
    DiagnosticRule("只读文件系统", "write_paths", None, "write to read-only filesystem"),
    DiagnosticRule("no write access", "write_paths", None, "write to directory"),
    DiagnosticRule("Permission denied", "write_paths", None, "permission denied on directory"),
    DiagnosticRule("Connection timed out", "network", {"network": True}, "network connection timeout"),
    DiagnosticRule("Temporary failure in name resolution", "network", {"network": True}, "DNS resolution failure"),
]


# ═══════════════════════════════════════════════════════════════════
# Variant Module — build (sys_prompt, tools) for each variant
# ═══════════════════════════════════════════════════════════════════


@dataclass
class Variant:
    """A benchmark variant: which sandbox_capabilities description + diagnostics."""

    name: str
    sandbox_desc: str
    add_diagnostic: bool = False

    def apply(self, baseline: dict) -> tuple[str, list[dict]]:
        """Return (system_prompt, tools_list) for this variant."""
        sys_prompt = baseline["system_prompt"]
        tools = copy.deepcopy(baseline["tool_schemas"])
        for tool in tools:
            if tool["name"] == "bash":
                params = tool["parameters"]
                if "properties" in params and "sandbox_capabilities" in params["properties"]:
                    params["properties"]["sandbox_capabilities"]["description"] = self.sandbox_desc
                break
        return sys_prompt, tools


VARIANTS = [
    Variant("control", ORIGINAL_SANDBOX_DESC),
    Variant("step1", NEW_SANDBOX_DESC),
    Variant("step2", NEW_SANDBOX_DESC, add_diagnostic=True),
    Variant("step2-only", ORIGINAL_SANDBOX_DESC, add_diagnostic=True),
]


# ═══════════════════════════════════════════════════════════════════
# Baseline Module — capture system prompt + tool schemas
# ═══════════════════════════════════════════════════════════════════


def capture_baseline() -> dict:
    """Run go run ./cmd/capture-baseline/ and return parsed JSON."""
    result = subprocess.run(
        CAPTURE_CMD,
        capture_output=True,
        text=True,
        cwd=str(REPO_ROOT),
        timeout=30,
    )
    if result.returncode != 0:
        print(f"STDERR: {result.stderr}", file=sys.stderr)
        raise RuntimeError(f"capture-baseline exited {result.returncode}")
    return json.loads(result.stdout)


# ═══════════════════════════════════════════════════════════════════
# Diagnostic Module — match error text → capability suggestion
# ═══════════════════════════════════════════════════════════════════


def match_diagnostic(stderr: str) -> DiagnosticRule | None:
    """Match stderr against keyword rules. Returns first match or None."""
    stderr_lower = stderr.lower()
    for rule in DIAG_RULES:
        if rule.keyword.lower() in stderr_lower:
            return rule
    return None


def format_diagnostic(rule: DiagnosticRule) -> str:
    """Build the diagnostic block appended to tool output."""
    cap_hint = f'add "{rule.cap_type}" to sandbox_capabilities'
    if rule.suggested_cap:
        cap_hint += f", e.g. {json.dumps(rule.suggested_cap, indent=6)}"
    return (
        f"\n\n--- sandbox diagnostic ---\n"
        f"⛔ This error may be caused by sandbox restrictions.\n"
        f"   Matched pattern: \"{rule.keyword}\"\n"
        f"   Suggestion: {cap_hint}\n"
        f"--- end sandbox diagnostic ---"
    )


# ═══════════════════════════════════════════════════════════════════
# Simulator Module — build simulated tool responses
# ═══════════════════════════════════════════════════════════════════


def build_error_content(scenario: SandboxScenario, add_diagnostic: bool = False) -> str:
    """Build the tool result content for a sandbox failure.

    Appends a diagnostic block when add_diagnostic is True and a matching
    rule exists for the scenario's sandbox error.
    """
    content = f"error: command exited: exit status {scenario.sandbox_exit_code}\n{scenario.sandbox_error}"
    if add_diagnostic:
        rule = match_diagnostic(scenario.sandbox_error)
        if rule:
            content += format_diagnostic(rule)
    return content


# ═══════════════════════════════════════════════════════════════════
# LLM Module — communication with the API
# ═══════════════════════════════════════════════════════════════════


def call_llm(
    client: Any,
    model: str,
    messages: list[dict],
    tools: list[dict],
    max_tokens: int = 4096,
) -> dict:
    """Send a chat completion request and return the response."""
    kwargs: dict[str, Any] = {
        "model": model,
        "messages": messages,
        "max_tokens": max_tokens,
    }
    if tools:
        kwargs["tools"] = [
            {
                "type": "function",
                "function": {
                    "name": t["name"],
                    "description": t["description"],
                    "parameters": t["parameters"],
                },
            }
            for t in tools
        ]
    return client.chat.completions.create(**kwargs)


def extract_tool_call(response: dict) -> tuple[str | None, dict | None, str | None]:
    """Extract (tool_name, tool_args, tool_call_id) from LLM response."""
    choice = response.choices[0]
    msg = choice.message
    if msg.tool_calls:
        tc = msg.tool_calls[0]
        try:
            args = json.loads(tc.function.arguments)
        except json.JSONDecodeError:
            args = {}
        return tc.function.name, args, tc.id
    # No tool call — text-only response
    return None, None, None


def extract_sandbox_cap(args: dict | None) -> dict | None:
    """Extract sandbox_capabilities from tool arguments."""
    if args and "sandbox_capabilities" in args:
        return args["sandbox_capabilities"]
    return None


def cap_is_correct(caps: dict | None, expected_cap: str) -> bool:
    """Check if the requested capabilities include the expected type with a non-null value."""
    if caps is None:
        return False
    return expected_cap in caps and caps[expected_cap] is not None


# ═══════════════════════════════════════════════════════════════════
# Eval Module — evaluation strategies
# ═══════════════════════════════════════════════════════════════════


@dataclass
class TurnRecord:
    """One turn in a multi-turn conversation log."""

    turn: int
    role: str
    content: str | None
    tool_name: str | None = None
    tool_args: dict | None = None


@dataclass
class EvalResult:
    """Unified evaluation result produced by either strategy."""

    scenario_id: str
    variant: str
    lang: str
    success: bool
    had_cap: bool
    cap_type_correct: bool
    recovery_turns: int
    first_retry_correct: bool
    requested_cap: dict | None
    token_usage: dict | None
    tool_used: str | None
    conversation: list[TurnRecord] = field(default_factory=list)
    error: str | None = None


# ── Helpers ──


def _inspect_tool_call(
    tool_name: str | None, tool_args: dict | None, scenario: SandboxScenario
) -> tuple[bool, bool, dict | None]:
    """Returns (had_cap, cap_type_correct, requested_cap)."""
    if tool_name == "bash":
        caps = extract_sandbox_cap(tool_args)
        return (
            caps is not None,
            cap_is_correct(caps, scenario.expected_cap),
            caps,
        )
    return False, False, None


# ── Single-turn strategy ──


def _build_single_turn_messages(
    sys_prompt: str,
    user_prompt: str,
    scenario: SandboxScenario,
    add_diagnostic: bool = False,
) -> list[dict]:
    """Build conversation: system + user + bash(fail) → ?"""
    tool_call_id = "call_fake_1234"
    error_content = build_error_content(scenario, add_diagnostic)
    return [
        {"role": "system", "content": sys_prompt},
        {"role": "user", "content": user_prompt},
        {
            "role": "assistant",
            "content": None,
            "tool_calls": [
                {
                    "id": tool_call_id,
                    "type": "function",
                    "function": {
                        "name": "bash",
                        "arguments": json.dumps({"command": scenario.bash_command}),
                    },
                }
            ],
        },
        {
            "role": "tool",
            "tool_call_id": tool_call_id,
            "content": error_content,
        },
    ]


def run_single_turn(
    client: Any,
    model: str,
    sys_prompt: str,
    tools: list[dict],
    scenario: SandboxScenario,
    lang: str,
    variant: Variant,
    show_input: bool = False,
) -> EvalResult:
    """One API call: predict the next action after a sandbox failure."""
    prompt = scenario.prompt(lang)
    messages = _build_single_turn_messages(sys_prompt, prompt, scenario, variant.add_diagnostic)

    if show_input:
        _print_request(messages, tools)

    try:
        resp = call_llm(client, model, messages, tools, max_tokens=1024)
        token_usage = {
            "prompt": resp.usage.prompt_tokens if resp.usage else 0,
            "completion": resp.usage.completion_tokens if resp.usage else 0,
        }
        tool_name, tool_args, _ = extract_tool_call(resp)
        had_cap, cap_correct, caps = _inspect_tool_call(tool_name, tool_args, scenario)

        error: str | None = None
        if tool_name is None:
            error = "text only response, no tool call"
        elif tool_name != "bash":
            error = f"switched to {tool_name}"
        elif not had_cap:
            error = "retried bash without sandbox_capabilities"

        return EvalResult(
            scenario_id=scenario.id,
            variant=variant.name,
            lang=lang,
            success=cap_correct,
            had_cap=had_cap,
            cap_type_correct=cap_correct,
            recovery_turns=0,
            first_retry_correct=False,
            requested_cap=caps,
            token_usage=token_usage,
            tool_used=tool_name,
            error=error,
        )
    except Exception as e:
        return EvalResult(
            scenario_id=scenario.id,
            variant=variant.name,
            lang=lang,
            success=False,
            had_cap=False,
            cap_type_correct=False,
            recovery_turns=0,
            first_retry_correct=False,
            requested_cap=None,
            token_usage=None,
            tool_used=None,
            error=str(e),
        )


# ── Multi-turn strategy ──


def run_scenario(
    client: Any,
    model: str,
    sys_prompt: str,
    tools: list[dict],
    scenario: SandboxScenario,
    lang: str,
    variant: Variant,
    max_turns: int = 6,
) -> EvalResult:
    """Run a full multi-turn conversation: user → bash(fail) → LLM retry → verdict."""
    user_prompt = scenario.prompt(lang)
    messages = [
        {"role": "system", "content": sys_prompt},
        {"role": "user", "content": user_prompt},
    ]

    sandbox_failed = False
    recovery_start_turn = 0
    retry_attempts = 0
    conversation: list[TurnRecord] = []
    token_usage = {"prompt": 0, "completion": 0}
    first_retry_correct = False
    first_requested_cap: dict | None = None

    for turn in range(max_turns):
        try:
            resp = call_llm(client, model, messages, tools)
            if resp.usage:
                token_usage["prompt"] += resp.usage.prompt_tokens or 0
                token_usage["completion"] += resp.usage.completion_tokens or 0

            tool_name, tool_args, tool_call_id = extract_tool_call(resp)
            conversation.append(
                TurnRecord(
                    turn=turn,
                    role="assistant",
                    content=resp.choices[0].message.content,
                    tool_name=tool_name,
                    tool_args=tool_args,
                )
            )

            if tool_name != "bash":
                # LLM used a different tool or gave a text-only response
                return EvalResult(
                    scenario_id=scenario.id,
                    variant=variant.name,
                    lang=lang,
                    success=False,
                    had_cap=False,
                    cap_type_correct=False,
                    recovery_turns=0,
                    first_retry_correct=first_retry_correct,
                    requested_cap=first_requested_cap,
                    token_usage=token_usage,
                    tool_used=tool_name,
                    conversation=conversation,
                    error=f"used non-bash tool: {tool_name}"
                    if tool_name
                    else "no tool call, text only response",
                )

            had_cap, cap_correct, caps = _inspect_tool_call(tool_name, tool_args, scenario)

            # ── First bash call: inject sandbox error, let LLM retry ──
            if not sandbox_failed:
                error_content = build_error_content(scenario, variant.add_diagnostic)
                messages.append(
                    {
                        "role": "assistant",
                        "content": None,
                        "tool_calls": [
                            {
                                "id": tool_call_id,
                                "type": "function",
                                "function": {
                                    "name": tool_name,
                                    "arguments": json.dumps(tool_args),
                                },
                            }
                        ],
                    }
                )
                messages.append(
                    {"role": "tool", "tool_call_id": tool_call_id, "content": error_content}
                )
                conversation.append(
                    TurnRecord(turn=turn, role="tool", content=error_content, tool_name="bash")
                )
                sandbox_failed = True
                recovery_start_turn = turn
                continue

            # ── Retry after sandbox failure ──
            retry_attempts += 1
            if retry_attempts == 1:
                first_retry_correct = cap_correct
                first_requested_cap = caps

            if cap_correct:
                return EvalResult(
                    scenario_id=scenario.id,
                    variant=variant.name,
                    lang=lang,
                    success=True,
                    had_cap=had_cap,
                    cap_type_correct=True,
                    recovery_turns=turn - recovery_start_turn,
                    first_retry_correct=first_retry_correct,
                    requested_cap=first_requested_cap,
                    token_usage=token_usage,
                    tool_used="bash",
                    conversation=conversation,
                )

            # Retried without the correct capability — fail
            return EvalResult(
                scenario_id=scenario.id,
                variant=variant.name,
                lang=lang,
                success=False,
                had_cap=had_cap,
                cap_type_correct=cap_correct,
                recovery_turns=turn - recovery_start_turn,
                first_retry_correct=first_retry_correct,
                requested_cap=first_requested_cap,
                token_usage=token_usage,
                tool_used="bash",
                conversation=conversation,
                error="retried without correct sandbox capabilities",
            )

        except Exception as e:
            return EvalResult(
                scenario_id=scenario.id,
                variant=variant.name,
                lang=lang,
                success=False,
                had_cap=False,
                cap_type_correct=False,
                recovery_turns=0,
                first_retry_correct=first_retry_correct,
                requested_cap=first_requested_cap,
                token_usage=token_usage,
                tool_used=None,
                conversation=conversation,
                error=str(e),
            )

    return EvalResult(
        scenario_id=scenario.id,
        variant=variant.name,
        lang=lang,
        success=False,
        had_cap=False,
        cap_type_correct=False,
        recovery_turns=0,
        first_retry_correct=first_retry_correct,
        requested_cap=first_requested_cap,
        token_usage=token_usage,
        tool_used=None,
        conversation=conversation,
        error=f"exceeded max {max_turns} turns",
    )


# ═══════════════════════════════════════════════════════════════════
# Report Module
# ═══════════════════════════════════════════════════════════════════

LANG_LABEL = {"zh": "中文", "en": "English"}


def _print_request(messages: list[dict], tools: list[dict]):
    """Print the API request for debugging."""
    print(f"\n{'=' * 70}")
    print("API Request:")
    print(f"{'=' * 70}")
    print("messages:")
    for i, m in enumerate(messages):
        if m["role"] == "system":
            print(f"  [{i}] system: ({len(m['content'])} chars, first 100: {m['content'][:100]}...)")
        elif m["role"] == "user":
            print(f"  [{i}] user: {m['content'][:120]}")
        elif m["role"] == "assistant" and m.get("tool_calls"):
            tc = m["tool_calls"][0]
            print(
                f"  [{i}] assistant → tool_call: {tc['function']['name']}"
                f"({tc['function']['arguments'][:200]})"
            )
        elif m["role"] == "tool":
            print(f"  [{i}] tool ({m.get('tool_call_id', '?')}): {m['content'][:200]}")
    for t in tools:
        if t["name"] == "bash":
            desc = (
                t["parameters"]
                .get("properties", {})
                .get("sandbox_capabilities", {})
                .get("description", "")
            )
            print(f"\nbash tool → sandbox_capabilities.description ({len(desc)} chars):")
            print(f"  {desc[:300]}{'...' if len(desc) > 300 else ''}")
            break
    print()


def report(results: list[EvalResult]):
    """Print a summary table grouped by language, then a variant-level summary."""
    langs = ["zh", "en"]

    print("=" * 90)
    print("  Benchmark: sandbox_capabilities description & diagnostic effect")
    print("=" * 90)

    for lang in langs:
        lang_results = [r for r in results if r.lang == lang]
        if not lang_results:
            continue
        print(f"\n--- {LANG_LABEL[lang]} ---")
        header = (
            f"{'Scenario':<20} {'Variant':<10} {'Success':>8} "
            f"{'CapRate':>8} {'Correct':>8} {'Recov':>6} {'1stOK':>6}"
        )
        print(header)
        print("-" * 66)

        for r in lang_results:
            print(
                f"{r.scenario_id:<20} {r.variant:<10} "
                f"{'✅' if r.success else '❌':>8} "
                f"{'✅' if r.had_cap else '❌':>8} "
                f"{'✅' if r.cap_type_correct else '❌':>8} "
                f"{r.recovery_turns:>6} "
                f"{'✅' if r.first_retry_correct else '❌':>6}"
            )
            if r.error:
                print(f"  ⚠ {r.error}")

    # Variant-level summary
    print("\n--- 汇总 ---")
    for variant in ["control", "step1", "step2", "step2-only"]:
        v_results = [r for r in results if r.variant == variant]
        if not v_results:
            continue
        total = len(v_results)
        success = sum(1 for r in v_results if r.success)
        had_cap = sum(1 for r in v_results if r.had_cap)
        correct = sum(1 for r in v_results if r.cap_type_correct)
        first_ok = sum(1 for r in v_results if r.first_retry_correct)
        print(f"  {variant}:")
        print(f"    Success:      {success}/{total} ({success / total * 100:.0f}%)")
        print(f"    Any cap:      {had_cap}/{total} ({had_cap / total * 100:.0f}%)")
        print(f"    Correct type: {correct}/{total} ({correct / total * 100:.0f}%)")
        print(f"    1st retry OK: {first_ok}/{total} ({first_ok / total * 100:.0f}%)")


# ═══════════════════════════════════════════════════════════════════
# Main
# ═══════════════════════════════════════════════════════════════════


def _resolve_env() -> str:
    """Load API key from .env or environment."""
    from dotenv import load_dotenv

    load_dotenv(dotenv_path=REPO_ROOT / ".env")
    return os.getenv("DEEPSEEK_API_KEY") or os.getenv("OPENAI_API_KEY") or ""


def main():
    parser = argparse.ArgumentParser(description="Sandbox capability benchmark")
    parser.add_argument("--model", default="deepseek-chat", help="Model name")
    parser.add_argument("--base-url", default="https://api.deepseek.com/v1", help="API base URL")
    parser.add_argument("--variants", nargs="+", help="Run only specific variants by name")
    parser.add_argument("--scenarios", nargs="+", help="Run only specific scenarios by ID")
    parser.add_argument("--lang", choices=["zh", "en", "both"], default="both")
    parser.add_argument("--dry-run", action="store_true", help="Print variants without calling LLM")
    parser.add_argument("--quick", action="store_true", help="Single-turn evaluation")
    parser.add_argument("--trials", type=int, default=5, help="Trials per cell (quick mode)")
    parser.add_argument("--show-input", action="store_true", help="Print the API request body")
    args = parser.parse_args()

    print("📸 Capturing baseline system prompt + tool schemas...", file=sys.stderr)
    baseline = capture_baseline()
    print(f"   System prompt: {baseline['prompt_length']} bytes", file=sys.stderr)
    print(f"   Tools:         {baseline['num_tools']} registered", file=sys.stderr)

    # ── Dry-run: show variants without API calls ──
    if args.dry_run:
        print("\n🔍 Dry-run: comparing variant descriptions")
        print("-" * 60)
        for variant in VARIANTS:
            _, tools = variant.apply(baseline)
            for t in tools:
                if t["name"] == "bash":
                    desc = (
                        t["parameters"]
                        .get("properties", {})
                        .get("sandbox_capabilities", {})
                        .get("description", "")
                    )
                    print(f"\n{variant.name} — sandbox_capabilities.description:")
                    if len(desc) > 200:
                        print(f"  {desc[:100]}...")
                        print(f"  ...{desc[-100:]}")
                    else:
                        print(f"  {desc}")
                    print(f"  (length: {len(desc)} chars, diagnostic: {variant.add_diagnostic})")
        print("\n✅ Dry-run complete. No API calls were made.")
        return

    # ── Resolve API key ──
    api_key = _resolve_env()
    if not api_key:
        print("❌ No API key found. Set DEEPSEEK_API_KEY in .env or environment.", file=sys.stderr)
        sys.exit(1)

    from openai import OpenAI

    client = OpenAI(api_key=api_key, base_url=args.base_url)

    # Select targets
    variants = [v for v in VARIANTS if not args.variants or v.name in args.variants]
    scenarios = [s for s in SCENARIOS if not args.scenarios or s.id in args.scenarios]
    langs = ["zh", "en"] if args.lang == "both" else [args.lang]

    # ── Quick mode: single-turn, N trials ──
    if args.quick:
        total = len(variants) * len(scenarios) * len(langs) * args.trials
        count = 0
        results: list[EvalResult] = []
        print(f"\n⚡ Quick evaluation: {total} calls ({args.trials} trials × {len(variants)} variants × {len(scenarios)} scenarios × {len(langs)} langs)", file=sys.stderr)
        for variant in variants:
            sys_prompt, tools = variant.apply(baseline)
            for scenario in scenarios:
                for lang in langs:
                    for t in range(args.trials):
                        count += 1
                        print(f"  [{count}/{total}] {variant.name}/{scenario.id}/{lang}#{t+1}...", file=sys.stderr, end="")
                        r = run_single_turn(
                            client, args.model, sys_prompt, tools,
                            scenario, lang, variant, show_input=args.show_input,
                        )
                        results.append(r)
                        print(" ✅" if r.success else " ❌", file=sys.stderr)
        prefix = "step1-benchmark-quick-"

    # ── Multi-turn mode ──
    else:
        total = len(variants) * len(scenarios) * len(langs)
        count = 0
        results = []
        print(f"\n🏃 Running {total} tests...", file=sys.stderr)
        for variant in variants:
            sys_prompt, tools = variant.apply(baseline)
            for scenario in scenarios:
                for lang in langs:
                    count += 1
                    print(f"  [{count}/{total}] {variant.name}/{scenario.id}/{lang}...", file=sys.stderr, end="")
                    r = run_scenario(
                        client, args.model, sys_prompt, tools,
                        scenario, lang, variant,
                    )
                    results.append(r)
                    print(" ✅" if r.success else " ❌", file=sys.stderr)
        prefix = "step1-benchmark-multiturn-"

    report(results)

    # Save JSON
    tmp = tempfile.NamedTemporaryFile(mode="w", suffix=".json", prefix=prefix, delete=False)
    json.dump(
        [
            {
                "scenario_id": r.scenario_id,
                "variant": r.variant,
                "lang": r.lang,
                "success": r.success,
                "had_cap": r.had_cap,
                "cap_type_correct": r.cap_type_correct,
                "recovery_turns": r.recovery_turns,
                "first_retry_correct": r.first_retry_correct,
                "requested_cap": r.requested_cap,
                "tool_used": r.tool_used,
                "token_usage": r.token_usage,
                "error": r.error,
            }
            for r in results
        ],
        tmp,
        indent=2,
        ensure_ascii=False,
    )
    tmp.close()
    print(f"\n📊 Results saved to: {tmp.name}", file=sys.stderr)


if __name__ == "__main__":
    main()
