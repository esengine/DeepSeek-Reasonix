use chrono::{DateTime, Local, TimeZone};

use crate::scene::{
    BoxLayout, Color, Dim, FillToken, FlexDirection, SceneFrame, SceneNode, TextRun, TextStyle,
};
use crate::state::{SceneCard, SceneState, SessionItem, SetupState, SlashMatch, ToolStatus};
use crate::theme::palette;

const MAX_CARD_BODY_LINES: usize = 5;
const MAX_SLASH_ROWS: usize = 6;
const MAX_SESSION_ROWS: usize = 8;
const APPROVAL_PROMPT_MAX: usize = 60;

const LOGO_LINES: [&str; 6] = [
    "██████╗ ███████╗ █████╗ ███████╗ ██████╗ ███╗   ██╗██╗██╗  ██╗",
    "██╔══██╗██╔════╝██╔══██╗██╔════╝██╔═══██╗████╗  ██║██║╚██╗██╔╝",
    "██████╔╝█████╗  ███████║███████╗██║   ██║██╔██╗ ██║██║ ╚███╔╝ ",
    "██╔══██╗██╔══╝  ██╔══██║╚════██║██║   ██║██║╚██╗██║██║ ██╔██╗ ",
    "██║  ██║███████╗██║  ██║███████║╚██████╔╝██║ ╚████║██║██╔╝ ██╗",
    "╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚══════╝ ╚═════╝ ╚═╝  ╚═══╝╚═╝╚═╝  ╚═╝",
];

pub fn build_trace_frame(state: &SceneState, cols: u16, rows: u16) -> SceneFrame {
    SceneFrame {
        schema_version: 1,
        cols: cols as u32,
        rows: rows as u32,
        root: outer_box(state),
    }
}

pub fn build_setup_frame(state: &SetupState, cols: u16, rows: u16) -> SceneFrame {
    SceneFrame {
        schema_version: 1,
        cols: cols as u32,
        rows: rows as u32,
        root: setup_root(state),
    }
}

fn outer_box(state: &SceneState) -> SceneNode {
    column(vec![scroll_area(state), dock(state)], BoxLayout::default())
}

fn scroll_area(state: &SceneState) -> SceneNode {
    let mut children: Vec<SceneNode> = Vec::new();
    if state.cards.is_empty() {
        children.extend(boot_block(state));
    } else {
        for card in &state.cards {
            children.push(card_block(card));
        }
    }
    column(
        children,
        BoxLayout {
            height: Some(Dim::Fill(FillToken::Fill)),
            width: Some(Dim::Fill(FillToken::Fill)),
            ..Default::default()
        },
    )
}

fn boot_block(state: &SceneState) -> Vec<SceneNode> {
    let mut rows: Vec<SceneNode> = Vec::new();
    rows.push(blank_row());
    for line in LOGO_LINES.iter() {
        rows.push(text(vec![styled_run(
            line,
            TextStyle {
                color: Some(palette::ds()),
                bold: Some(true),
                ..Default::default()
            },
        )]));
    }
    rows.push(blank_row());
    rows.push(text(vec![
        styled_run(
            " DeepSeek code agent  ",
            TextStyle {
                color: Some(palette::fg()),
                ..Default::default()
            },
        ),
        styled_run(
            "· terminal-native, cache-first ·",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
    ]));
    rows.push(blank_row());
    if let Some(model) = state.model.as_deref() {
        rows.push(boot_field("model", model, palette::ds_bright()));
    }
    if let Some(cwd) = state.cwd.as_deref() {
        rows.push(boot_field("workdir", cwd, palette::fg()));
    }
    if let Some(n) = state.mcp_server_count {
        if n > 0 {
            rows.push(boot_field("mcp", &format!("{} server(s) connected", n), palette::fg()));
        }
    }
    rows.push(boot_field(
        "tools",
        "read · write · edit · bash · grep · fetch · todo",
        palette::fg(),
    ));
    rows.push(blank_row());
    rows.push(text(vec![
        plain_run(" ", TextStyle::default()),
        styled_run(
            "type to chat  ",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            "·  ",
            TextStyle {
                color: Some(palette::fg3()),
                ..Default::default()
            },
        ),
        styled_run(
            "/",
            TextStyle {
                color: Some(palette::ds()),
                bold: Some(true),
                ..Default::default()
            },
        ),
        styled_run(
            " commands  ",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            "·  ",
            TextStyle {
                color: Some(palette::fg3()),
                ..Default::default()
            },
        ),
        styled_run(
            "@",
            TextStyle {
                color: Some(palette::ds()),
                bold: Some(true),
                ..Default::default()
            },
        ),
        styled_run(
            " file refs  ",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            "·  ",
            TextStyle {
                color: Some(palette::fg3()),
                ..Default::default()
            },
        ),
        styled_run(
            "!",
            TextStyle {
                color: Some(palette::ds()),
                bold: Some(true),
                ..Default::default()
            },
        ),
        styled_run(
            " shell  ",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            "·  ",
            TextStyle {
                color: Some(palette::fg3()),
                ..Default::default()
            },
        ),
        styled_run(
            "Ctrl+C",
            TextStyle {
                color: Some(palette::ds()),
                bold: Some(true),
                ..Default::default()
            },
        ),
        styled_run(
            " cancel  ",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            "·  ",
            TextStyle {
                color: Some(palette::fg3()),
                ..Default::default()
            },
        ),
        styled_run(
            "Ctrl+D",
            TextStyle {
                color: Some(palette::ds()),
                bold: Some(true),
                ..Default::default()
            },
        ),
        styled_run(
            " exit",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
    ]));
    rows
}

fn boot_field(key: &str, value: &str, value_color: Color) -> SceneNode {
    let key_str = format!(" {:<10}", key);
    text(vec![
        styled_run(
            &key_str,
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            value,
            TextStyle {
                color: Some(value_color),
                ..Default::default()
            },
        ),
    ])
}

fn card_block(c: &SceneCard) -> SceneNode {
    if c.kind == "tool" {
        return tool_card_block(c);
    }
    if c.kind == "user" || c.kind == "reasoning" || c.kind == "streaming" {
        return message_card_block(c);
    }
    let color = color_for(&c.kind);
    let label = kind_label(&c.kind);
    let mut runs = vec![
        styled_run(
            glyph_for(&c.kind),
            TextStyle {
                color: Some(color.clone()),
                bold: Some(true),
                ..Default::default()
            },
        ),
        plain_run(" ", TextStyle::default()),
    ];
    if let Some(label) = label {
        runs.push(styled_run(
            label,
            TextStyle {
                color: Some(color),
                bold: Some(true),
                ..Default::default()
            },
        ));
        runs.push(plain_run("  ", TextStyle::default()));
    }
    let summary = if c.summary.is_empty() { &c.kind } else { &c.summary };
    runs.push(styled_run(
        summary,
        TextStyle {
            color: Some(palette::fg()),
            ..Default::default()
        },
    ));
    text(runs)
}

fn tool_card_block(c: &SceneCard) -> SceneNode {
    let mut runs = vec![
        styled_run(
            "▸ ",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            if c.summary.is_empty() { "tool" } else { &c.summary },
            TextStyle {
                color: Some(palette::fg()),
                bold: Some(true),
                ..Default::default()
            },
        ),
    ];
    if let Some(args) = c.args.as_deref() {
        runs.push(styled_run(
            " (",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ));
        runs.push(styled_run(
            args,
            TextStyle {
                color: Some(palette::ds_bright()),
                ..Default::default()
            },
        ));
        runs.push(styled_run(
            ")",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ));
    }
    runs.push(plain_run("  ", TextStyle::default()));
    match c.status {
        Some(ToolStatus::Ok) => runs.push(styled_run(
            "✓",
            TextStyle {
                color: Some(palette::ok()),
                bold: Some(true),
                ..Default::default()
            },
        )),
        Some(ToolStatus::Err) => runs.push(styled_run(
            "✗",
            TextStyle {
                color: Some(palette::err()),
                bold: Some(true),
                ..Default::default()
            },
        )),
        _ => runs.push(styled_run(
            "…",
            TextStyle {
                color: Some(palette::warn()),
                ..Default::default()
            },
        )),
    }
    if let Some(elapsed) = c.elapsed.as_deref() {
        runs.push(styled_run(
            &format!(" {}", elapsed),
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ));
    }
    if let Some(id) = c.id.as_deref() {
        runs.push(styled_run(
            &format!("  {}", id),
            TextStyle {
                color: Some(palette::fg3()),
                ..Default::default()
            },
        ));
    }
    text(runs)
}

fn message_card_block(c: &SceneCard) -> SceneNode {
    let color = color_for(&c.kind);
    let label = kind_label(&c.kind).unwrap_or(&c.kind).to_string();
    let head = head_row(c, color.clone(), &label);
    let mut rows = vec![head];
    let body_source = c.body.clone().unwrap_or_else(|| c.summary.clone());
    for line in body_lines(&body_source) {
        rows.push(body_row(&line, &c.kind));
    }
    rows.push(blank_row());
    column(rows, BoxLayout::default())
}

fn head_row(c: &SceneCard, color: Color, label: &str) -> SceneNode {
    let left_runs = vec![
        styled_run(
            glyph_for(&c.kind),
            TextStyle {
                color: Some(color.clone()),
                bold: Some(true),
                ..Default::default()
            },
        ),
        plain_run(" ", TextStyle::default()),
        styled_run(
            label,
            TextStyle {
                color: Some(color),
                bold: Some(true),
                ..Default::default()
            },
        ),
    ];
    let mut right_runs: Vec<TextRun> = Vec::new();
    if let Some(meta) = c.meta.as_deref() {
        right_runs.push(styled_run(
            meta,
            TextStyle {
                color: Some(palette::fg3()),
                ..Default::default()
            },
        ));
    }
    if let Some(ts) = c.ts {
        if !right_runs.is_empty() {
            right_runs.push(styled_run(
                "  ·  ",
                TextStyle {
                    color: Some(palette::fg3()),
                    ..Default::default()
                },
            ));
        }
        right_runs.push(styled_run(
            &format_ts(ts),
            TextStyle {
                color: Some(palette::fg3()),
                ..Default::default()
            },
        ));
    }
    if right_runs.is_empty() {
        return text(left_runs);
    }
    row(
        vec![text(left_runs), spacer_box(), text(right_runs)],
        BoxLayout {
            direction: Some(FlexDirection::Row),
            ..Default::default()
        },
    )
}

fn body_row(line: &str, kind: &str) -> SceneNode {
    let style = if kind == "reasoning" {
        TextStyle {
            color: Some(palette::fg1()),
            italic: Some(true),
            ..Default::default()
        }
    } else {
        TextStyle {
            color: Some(palette::fg()),
            ..Default::default()
        }
    };
    text(vec![styled_run(&format!("  {}", line), style)])
}

fn body_lines(body: &str) -> Vec<String> {
    let mut out = Vec::new();
    for raw in body.split('\n') {
        let line = raw.trim_end();
        if line.is_empty() {
            continue;
        }
        out.push(line.to_string());
        if out.len() >= MAX_CARD_BODY_LINES {
            break;
        }
    }
    out
}

fn format_ts(ts: i64) -> String {
    let dt: DateTime<Local> = match Local.timestamp_millis_opt(ts) {
        chrono::offset::LocalResult::Single(dt) => dt,
        _ => return String::new(),
    };
    dt.format("%H:%M:%S").to_string()
}

fn glyph_for(kind: &str) -> &'static str {
    match kind {
        "user" => "❯",
        "reasoning" | "streaming" | "plan" | "task" => "◆",
        "tool" => "▸",
        "diff" => "Δ",
        "error" => "✗",
        "warn" => "!",
        _ => "·",
    }
}

fn color_for(kind: &str) -> Color {
    match kind {
        "user" => palette::ds(),
        "reasoning" | "diff" | "plan" | "task" => palette::ds_purple(),
        "streaming" => palette::ok(),
        "tool" => palette::fg1(),
        "error" => palette::err(),
        "warn" => palette::warn(),
        _ => palette::fg2(),
    }
}

fn kind_label(kind: &str) -> Option<&'static str> {
    match kind {
        "user" => Some("YOU"),
        "reasoning" => Some("THINKING"),
        "streaming" => Some("reasonix"),
        _ => None,
    }
}

fn dock(state: &SceneState) -> SceneNode {
    let mut children: Vec<SceneNode> = Vec::new();
    let has_sessions = state
        .sessions
        .as_ref()
        .map(|s| !s.is_empty())
        .unwrap_or(false);
    let has_slash = state
        .slash_matches
        .as_ref()
        .map(|s| !s.is_empty())
        .unwrap_or(false);
    if has_sessions {
        children.extend(sessions_picker_block(state));
    } else if has_slash && state.approval_prompt.is_none() {
        children.extend(slash_overlay_block(state));
    }
    if let Some(prompt) = state.approval_prompt.as_deref() {
        children.push(approval_row(state.approval_kind.as_deref(), prompt));
    } else {
        children.push(composer_row(state));
    }
    children.push(meta_row());
    children.push(status_bar_row(state));
    column(
        children,
        BoxLayout {
            direction: Some(FlexDirection::Column),
            ..Default::default()
        },
    )
}

fn composer_row(state: &SceneState) -> SceneNode {
    let t = state.composer_text.as_deref().unwrap_or("");
    let mut runs = vec![
        plain_run(" ", TextStyle::default()),
        styled_run(
            "❯ ",
            TextStyle {
                color: Some(palette::ds()),
                bold: Some(true),
                ..Default::default()
            },
        ),
    ];
    if t.is_empty() {
        runs.push(styled_run(
            "type to chat · / for commands · @ for files",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ));
        return row(
            vec![text(runs)],
            BoxLayout {
                direction: Some(FlexDirection::Row),
                background: Some(palette::bg2()),
                height: Some(Dim::Cells(1)),
                ..Default::default()
            },
        );
    }
    let cursor = state
        .composer_cursor
        .map(|c| c.min(t.chars().count()))
        .unwrap_or_else(|| t.chars().count());
    let (before, after) = split_at_char(t, cursor);
    if !before.is_empty() {
        runs.push(styled_run(
            &before,
            TextStyle {
                color: Some(palette::fg()),
                ..Default::default()
            },
        ));
    }
    runs.push(styled_run(
        "▮",
        TextStyle {
            color: Some(palette::ds()),
            ..Default::default()
        },
    ));
    if !after.is_empty() {
        runs.push(styled_run(
            &after,
            TextStyle {
                color: Some(palette::fg()),
                ..Default::default()
            },
        ));
    }
    row(
        vec![text(runs)],
        BoxLayout {
            direction: Some(FlexDirection::Row),
            background: Some(palette::bg2()),
            height: Some(Dim::Cells(1)),
            ..Default::default()
        },
    )
}

fn split_at_char(s: &str, char_idx: usize) -> (String, String) {
    let mut before = String::new();
    let mut after = String::new();
    for (i, ch) in s.chars().enumerate() {
        if i < char_idx {
            before.push(ch);
        } else {
            after.push(ch);
        }
    }
    (before, after)
}

fn meta_row() -> SceneNode {
    let left = text(vec![
        plain_run(" ", TextStyle::default()),
        styled_run(
            "↵",
            TextStyle {
                color: Some(palette::ds()),
                ..Default::default()
            },
        ),
        styled_run(
            " send  ",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            "⇧↵",
            TextStyle {
                color: Some(palette::ds()),
                ..Default::default()
            },
        ),
        styled_run(
            " newline  ",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            "/",
            TextStyle {
                color: Some(palette::ds()),
                ..Default::default()
            },
        ),
        styled_run(
            " cmd  ",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            "@",
            TextStyle {
                color: Some(palette::ds()),
                ..Default::default()
            },
        ),
        styled_run(
            " file  ",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            "!",
            TextStyle {
                color: Some(palette::ds()),
                ..Default::default()
            },
        ),
        styled_run(
            " shell",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
    ]);
    let right = text(vec![
        styled_run(
            "esc",
            TextStyle {
                color: Some(palette::ds()),
                ..Default::default()
            },
        ),
        styled_run(
            " cancel  ",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            "↑",
            TextStyle {
                color: Some(palette::ds()),
                ..Default::default()
            },
        ),
        styled_run(
            " history ",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
    ]);
    row(
        vec![left, spacer_box(), right],
        BoxLayout {
            direction: Some(FlexDirection::Row),
            height: Some(Dim::Cells(1)),
            ..Default::default()
        },
    )
}

fn status_bar_row(state: &SceneState) -> SceneNode {
    let mut children: Vec<SceneNode> = Vec::new();
    children.push(text(vec![
        styled_run(
            " ●",
            TextStyle {
                color: Some(palette::ok()),
                ..Default::default()
            },
        ),
        styled_run(
            " reasonix",
            TextStyle {
                bold: Some(true),
                color: Some(palette::fg()),
                ..Default::default()
            },
        ),
    ]));
    if let Some(model) = state.model.as_deref() {
        children.push(text(vec![
            styled_run(
                "  model ",
                TextStyle {
                    color: Some(palette::fg2()),
                    ..Default::default()
                },
            ),
            styled_run(
                model,
                TextStyle {
                    color: Some(palette::ds()),
                    ..Default::default()
                },
            ),
        ]));
    }
    if let Some(mode) = state.edit_mode.as_ref() {
        let mode_color = match mode {
            crate::state::EditMode::Yolo => palette::err(),
            crate::state::EditMode::Auto => palette::warn(),
            crate::state::EditMode::Review => palette::ds(),
        };
        children.push(text(vec![
            styled_run(
                "  mode ",
                TextStyle {
                    color: Some(palette::fg2()),
                    ..Default::default()
                },
            ),
            styled_run(
                mode.as_str(),
                TextStyle {
                    color: Some(mode_color),
                    bold: Some(true),
                    ..Default::default()
                },
            ),
        ]));
    }
    children.push(text(vec![
        plain_run("  ", TextStyle::default()),
        styled_run(
            if state.busy { "busy" } else { "idle" },
            TextStyle {
                color: Some(if state.busy { palette::warn() } else { palette::ok() }),
                ..Default::default()
            },
        ),
    ]));
    if let Some(activity) = state.activity.as_deref() {
        children.push(text(vec![styled_run(
            &format!(" · {}", activity),
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        )]));
    }
    children.push(spacer_box());
    if let Some(wallet) = format_wallet(state.wallet_balance, state.wallet_currency.as_deref()) {
        children.push(text(vec![
            styled_run(
                "wallet ",
                TextStyle {
                    color: Some(palette::fg2()),
                    ..Default::default()
                },
            ),
            styled_run(
                &format!("{} ", wallet),
                TextStyle {
                    color: Some(palette::ok()),
                    bold: Some(true),
                    ..Default::default()
                },
            ),
        ]));
    }
    if let Some(cwd) = state.cwd.as_deref() {
        children.push(text(vec![
            styled_run(
                "cwd ",
                TextStyle {
                    color: Some(palette::fg2()),
                    ..Default::default()
                },
            ),
            styled_run(
                &format!("{} ", trunc_cwd(cwd)),
                TextStyle {
                    color: Some(palette::fg1()),
                    ..Default::default()
                },
            ),
        ]));
    }
    row(
        children,
        BoxLayout {
            direction: Some(FlexDirection::Row),
            background: Some(palette::bg2()),
            height: Some(Dim::Cells(1)),
            ..Default::default()
        },
    )
}

fn trunc_cwd(cwd: &str) -> String {
    if cwd.chars().count() <= 30 {
        return cwd.to_string();
    }
    let tail: String = cwd.chars().rev().take(29).collect::<Vec<_>>().into_iter().rev().collect();
    format!("…{}", tail)
}

fn format_wallet(total: Option<f64>, currency: Option<&str>) -> Option<String> {
    let total = total?;
    if !total.is_finite() {
        return None;
    }
    let symbol = currency_symbol(currency);
    Some(format!("{}{:.2}", symbol, total))
}

fn currency_symbol(currency: Option<&str>) -> String {
    match currency.map(|c| c.to_ascii_uppercase()) {
        Some(ref c) if c == "CNY" || c == "RMB" || c == "JPY" => "¥".to_string(),
        Some(ref c) if c == "USD" => "$".to_string(),
        Some(ref c) if c == "EUR" => "€".to_string(),
        Some(ref c) if c == "GBP" => "£".to_string(),
        Some(c) if !c.is_empty() => format!("{} ", c),
        _ => String::new(),
    }
}

fn approval_row(kind: Option<&str>, prompt: &str) -> SceneNode {
    let clipped: String = if prompt.chars().count() > APPROVAL_PROMPT_MAX {
        let head: String = prompt.chars().take(APPROVAL_PROMPT_MAX - 1).collect();
        format!("{}…", head)
    } else {
        prompt.to_string()
    };
    let mut runs = vec![styled_run(
        " ❓ ",
        TextStyle {
            color: Some(palette::warn()),
            bold: Some(true),
            ..Default::default()
        },
    )];
    if let Some(kind) = kind {
        runs.push(styled_run(
            &format!("[{}] ", kind),
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ));
    }
    runs.push(styled_run(
        &clipped,
        TextStyle {
            color: Some(palette::fg()),
            ..Default::default()
        },
    ));
    runs.push(styled_run(
        "  [y/n]",
        TextStyle {
            color: Some(palette::warn()),
            bold: Some(true),
            ..Default::default()
        },
    ));
    row(
        vec![text(runs)],
        BoxLayout {
            direction: Some(FlexDirection::Row),
            background: Some(palette::bg2()),
            height: Some(Dim::Cells(1)),
            ..Default::default()
        },
    )
}

fn slash_overlay_block(state: &SceneState) -> Vec<SceneNode> {
    let matches = state.slash_matches.as_ref().unwrap();
    let sel = state
        .slash_selected_index
        .unwrap_or(0)
        .min(matches.len().saturating_sub(1));
    let (start, shown) = list_window(matches, sel, MAX_SLASH_ROWS);
    let mut rows: Vec<SceneNode> = Vec::new();
    let plural = if matches.len() == 1 { "" } else { "es" };
    rows.push(text(vec![
        plain_run(" ", TextStyle::default()),
        styled_run(
            "/",
            TextStyle {
                color: Some(palette::ds()),
                bold: Some(true),
                ..Default::default()
            },
        ),
        styled_run(
            " commands",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            &format!("  {} match{}", matches.len(), plural),
            TextStyle {
                color: Some(palette::fg3()),
                ..Default::default()
            },
        ),
    ]));
    for (i, m) in shown.iter().enumerate() {
        rows.push(slash_row(m, start + i == sel));
    }
    let hidden = matches.len() - shown.len();
    if hidden > 0 {
        rows.push(overflow_row(hidden));
    }
    rows
}

fn slash_row(m: &SlashMatch, selected: bool) -> SceneNode {
    let prefix = if selected { " ▸ " } else { "   " };
    let mut runs = vec![
        styled_run(
            prefix,
            TextStyle {
                color: Some(if selected { palette::ds() } else { palette::fg3() }),
                ..Default::default()
            },
        ),
        styled_run(
            &m.cmd,
            if selected {
                TextStyle {
                    bold: Some(true),
                    color: Some(palette::ds_bright()),
                    ..Default::default()
                }
            } else {
                TextStyle {
                    color: Some(palette::fg1()),
                    ..Default::default()
                }
            },
        ),
    ];
    if let Some(args_hint) = m.args_hint.as_deref() {
        runs.push(styled_run(
            &format!(" {}", args_hint),
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ));
    }
    if !m.summary.is_empty() {
        runs.push(plain_run("  ", TextStyle::default()));
        runs.push(styled_run(
            &m.summary,
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ));
    }
    text(runs)
}

fn sessions_picker_block(state: &SceneState) -> Vec<SceneNode> {
    let list = state.sessions.as_ref().unwrap();
    let sel = state
        .sessions_focused_index
        .unwrap_or(0)
        .min(list.len().saturating_sub(1));
    let (start, shown) = list_window(list, sel, MAX_SESSION_ROWS);
    let mut rows: Vec<SceneNode> = Vec::new();
    rows.push(text(vec![
        plain_run(" ", TextStyle::default()),
        styled_run(
            "◇",
            TextStyle {
                color: Some(palette::ds()),
                bold: Some(true),
                ..Default::default()
            },
        ),
        styled_run(
            " sessions",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            &format!("  {} saved", list.len()),
            TextStyle {
                color: Some(palette::fg3()),
                ..Default::default()
            },
        ),
    ]));
    for (i, s) in shown.iter().enumerate() {
        rows.push(session_row(s, start + i == sel));
    }
    let hidden = list.len() - shown.len();
    if hidden > 0 {
        rows.push(overflow_row(hidden));
    }
    rows.push(text(vec![
        plain_run(" ", TextStyle::default()),
        styled_run(
            "↑↓",
            TextStyle {
                color: Some(palette::ds()),
                ..Default::default()
            },
        ),
        styled_run(
            " navigate  ",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            "⏎",
            TextStyle {
                color: Some(palette::ds()),
                ..Default::default()
            },
        ),
        styled_run(
            " open  ",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            "n",
            TextStyle {
                color: Some(palette::ds()),
                ..Default::default()
            },
        ),
        styled_run(
            " new  ",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
        styled_run(
            "esc",
            TextStyle {
                color: Some(palette::ds()),
                ..Default::default()
            },
        ),
        styled_run(
            " cancel",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
    ]));
    rows
}

fn session_row(item: &SessionItem, focused: bool) -> SceneNode {
    let prefix = if focused { " ▸ " } else { "   " };
    let mut runs = vec![
        styled_run(
            prefix,
            TextStyle {
                color: Some(if focused { palette::ds() } else { palette::fg3() }),
                ..Default::default()
            },
        ),
        styled_run(
            &item.title,
            if focused {
                TextStyle {
                    bold: Some(true),
                    color: Some(palette::ds_bright()),
                    ..Default::default()
                }
            } else {
                TextStyle {
                    color: Some(palette::fg1()),
                    ..Default::default()
                }
            },
        ),
    ];
    if let Some(meta) = item.meta.as_deref() {
        runs.push(plain_run("  ", TextStyle::default()));
        runs.push(styled_run(
            meta,
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ));
    }
    text(runs)
}

fn overflow_row(hidden: usize) -> SceneNode {
    text(vec![styled_run(
        &format!("   …{} more", hidden),
        TextStyle {
            color: Some(palette::fg3()),
            ..Default::default()
        },
    )])
}

fn list_window<T>(items: &[T], selected: usize, window_size: usize) -> (usize, &[T]) {
    if items.len() <= window_size {
        return (0, items);
    }
    let half = window_size / 2;
    let max_start = items.len() - window_size;
    let start = selected.saturating_sub(half).min(max_start);
    (start, &items[start..start + window_size])
}

fn setup_root(state: &SetupState) -> SceneNode {
    let mut children: Vec<SceneNode> = Vec::new();
    children.push(text(vec![
        styled_run(
            " ● ",
            TextStyle {
                color: Some(palette::ds()),
                bold: Some(true),
                ..Default::default()
            },
        ),
        styled_run(
            "REASONIX",
            TextStyle {
                bold: Some(true),
                color: Some(palette::ds_bright()),
                ..Default::default()
            },
        ),
        styled_run(
            "  welcome",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ),
    ]));
    children.push(blank_row());
    children.push(text(vec![styled_run(
        " Enter your DeepSeek API key:",
        TextStyle {
            color: Some(palette::ds()),
            ..Default::default()
        },
    )]));
    children.push(text(vec![styled_run(
        "   get one at https://platform.deepseek.com",
        TextStyle {
            color: Some(palette::fg2()),
            ..Default::default()
        },
    )]));
    let mut masked = vec![styled_run(
        " ❯ ",
        TextStyle {
            color: Some(palette::ds()),
            bold: Some(true),
            ..Default::default()
        },
    )];
    if state.buffer_length == 0 {
        masked.push(styled_run(
            "(start typing your key)",
            TextStyle {
                color: Some(palette::fg2()),
                ..Default::default()
            },
        ));
    } else {
        let dots: String = "•".repeat(state.buffer_length);
        masked.push(styled_run(
            &dots,
            TextStyle {
                color: Some(palette::fg()),
                ..Default::default()
            },
        ));
        masked.push(styled_run(
            "▮",
            TextStyle {
                color: Some(palette::ds()),
                ..Default::default()
            },
        ));
    }
    children.push(text(masked));
    if let Some(err) = state.error.as_deref() {
        children.push(text(vec![
            styled_run(
                " ✗ ",
                TextStyle {
                    color: Some(palette::err()),
                    bold: Some(true),
                    ..Default::default()
                },
            ),
            styled_run(
                err,
                TextStyle {
                    color: Some(palette::err()),
                    ..Default::default()
                },
            ),
        ]));
    }
    children.push(blank_row());
    children.push(text(vec![styled_run(
        " Ctrl+C to exit · /exit to quit",
        TextStyle {
            color: Some(palette::fg2()),
            ..Default::default()
        },
    )]));
    column(children, BoxLayout::default())
}

fn column(children: Vec<SceneNode>, mut layout: BoxLayout) -> SceneNode {
    layout.direction = Some(layout.direction.unwrap_or(FlexDirection::Column));
    SceneNode::Box {
        layout: Some(layout),
        children,
    }
}

fn row(children: Vec<SceneNode>, mut layout: BoxLayout) -> SceneNode {
    layout.direction = Some(FlexDirection::Row);
    SceneNode::Box {
        layout: Some(layout),
        children,
    }
}

fn text(runs: Vec<TextRun>) -> SceneNode {
    SceneNode::Text { runs, wrap: None }
}

fn blank_row() -> SceneNode {
    text(vec![plain_run("", TextStyle::default())])
}

fn spacer_box() -> SceneNode {
    SceneNode::Box {
        layout: Some(BoxLayout {
            width: Some(Dim::Fill(FillToken::Fill)),
            ..Default::default()
        }),
        children: vec![],
    }
}

fn styled_run(text: &str, style: TextStyle) -> TextRun {
    TextRun {
        text: text.to_string(),
        style: Some(style),
    }
}

fn plain_run(text: &str, _style: TextStyle) -> TextRun {
    TextRun {
        text: text.to_string(),
        style: None,
    }
}

