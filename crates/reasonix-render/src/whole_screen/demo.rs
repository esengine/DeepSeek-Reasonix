use chrono::Local;

use crate::state::{SceneCard, SceneState, SlashMatch, ToolStatus};

pub fn demo_state() -> SceneState {
    let ts = Local::now().timestamp();
    SceneState {
        model: Some("deepseek-v3.2-coder".to_string()),
        cwd: Some("~/work/reasonix-core".to_string()),
        mcp_server_count: Some(0),
        composer_text: Some(String::new()),
        composer_cursor: Some(0),
        busy: true,
        activity: Some("streaming".to_string()),
        ctx_tokens: Some(19_200),
        ctx_cap: Some(128_000),
        session_cost_usd: Some(0.043),
        last_turn_cost_usd: Some(0.012),
        cache_hit_ratio: Some(0.87),
        last_turn_ms: Some(2_100),
        session_input_tokens: Some(12_408),
        session_output_tokens: Some(3_194),
        edit_mode: Some(crate::state::EditMode::Auto),
        preset: Some("pro".to_string()),
        slash_catalog: Some(
            [
                ("clear", "reset conversation context"),
                ("compact", "summarize history to free up tokens"),
                ("commit", "create a git commit from current changes"),
                ("diff", "show pending edits as a diff"),
                ("undo", "revert the last file edit"),
                ("help", "show help"),
            ]
            .iter()
            .map(|(cmd, summary)| SlashMatch {
                cmd: (*cmd).to_string(),
                summary: (*summary).to_string(),
                group: Some("chat".to_string()),
                args_hint: None,
                aliases: Vec::new(),
                arg_completer: None,
            })
            .collect(),
        ),
        cards: vec![
            SceneCard {
                kind: "user".to_string(),
                body: Some(
                    "帮我把 parser.ts 里的 token 流处理改成异步迭代器，顺便加点测试。".to_string(),
                ),
                ts: Some(ts),
                ..Default::default()
            },
            SceneCard {
                kind: "reasoning".to_string(),
                body: Some(
                    "需要先看下当前 parser.ts 结构，再决定怎么改。\n\
                     流式场景下 AsyncIterator 比 callback 更自然。\n\
                     调用方有 7 处用到 parseStream，要确认接口形态变化是否会破坏外层。"
                        .to_string(),
                ),
                meta: Some("8 steps · 2.1s".to_string()),
                ..Default::default()
            },
            SceneCard {
                kind: "todo".to_string(),
                body: Some(
                    "[x] 阅读 parser.ts 当前实现\n\
                     [x] 检查调用方依赖\n\
                     [~] 改写为 AsyncIterator 接口\n\
                     [ ] 迁移现有 callback 调用\n\
                     [ ] 编写 vitest 单元测试"
                        .to_string(),
                ),
                ..Default::default()
            },
            SceneCard {
                kind: "tool".to_string(),
                summary: "Read".to_string(),
                args: Some("src/parser.ts".to_string()),
                status: Some(ToolStatus::Ok),
                elapsed: Some("0.12s".to_string()),
                id: Some("#a4f1".to_string()),
                ..Default::default()
            },
            SceneCard {
                kind: "tool".to_string(),
                summary: "Grep".to_string(),
                args: Some("\"parseStream\", in: src/".to_string()),
                status: Some(ToolStatus::Ok),
                elapsed: Some("0.08s".to_string()),
                id: Some("#a4f2".to_string()),
                ..Default::default()
            },
            SceneCard {
                kind: "subagent".to_string(),
                summary: "subagent: code-reviewer".to_string(),
                meta: Some("3 steps · 1.4s".to_string()),
                body: Some(
                    "审查 parseStream 调用方是否依赖旧返回值形状\n\
                     scanned 7 call-sites in src/render/, src/parser.ts, tests/\n\
                     all callers expect AsyncIterable<Token>\n\
                     safe to rewrite, no caller migration needed"
                        .to_string(),
                ),
                ..Default::default()
            },
            SceneCard {
                kind: "tool".to_string(),
                summary: "Edit".to_string(),
                args: Some("src/parser.ts, +34 -18".to_string()),
                status: Some(ToolStatus::Running),
                ..Default::default()
            },
            SceneCard {
                kind: "fileview".to_string(),
                summary: "src/parser.ts".to_string(),
                meta: Some("36 more lines".to_string()),
                body: Some(
                    "14:import { escape } from \"./html\";\n\
                     15:\n\
                     16:// renders a single token to its display form\n\
                     17:export function formatToken(t: Token) {\n\
                     18:  const { kind, value } = t;\n\
                     19:  if (kind === \"text\") return value;\n\
                     20:  return `<${kind}>${value}</${kind}>`;\n\
                     21:}"
                        .to_string(),
                ),
                ..Default::default()
            },
            SceneCard {
                kind: "search".to_string(),
                summary: "grep \"parseStream\" in src/".to_string(),
                meta: Some("4 matches · 3 files".to_string()),
                body: Some(
                    "src/render/output.ts:42:return chunks.map(parseStream).join(\"\");\n\
                     src/render/output.ts:88:const out = parseStream(token);\n\
                     src/parser.ts:134:stream.push(parseStream(t));\n\
                     tests/format.spec.ts:11:expect(parseStream({kind:\"text\"})).toBe(\"hi\");"
                        .to_string(),
                ),
                ..Default::default()
            },
            SceneCard {
                kind: "cmd".to_string(),
                summary: "pnpm test parser".to_string(),
                meta: Some("exit 0 · 2.1s".to_string()),
                body: Some(
                    " RUN  v1.6.0 ~/work/reasonix-core\n\
                     \n\
                     stdout | parser.spec.ts > StreamParser\n\
                       ✓ yields tokens from async iterable        (3 ms)\n\
                       ✓ handles split chunks across boundaries    (5 ms)\n\
                     \n\
                     Test Files  1 passed (1)\n\
                          Tests  24 passed (24)"
                        .to_string(),
                ),
                ..Default::default()
            },
            SceneCard {
                kind: "streaming".to_string(),
                body: Some(
                    "已经把 parseStream 改成 async *parse() 异步生成器，并把 7 处调用迁移完成。"
                        .to_string(),
                ),
                ..Default::default()
            },
        ],
        ..Default::default()
    }
}
