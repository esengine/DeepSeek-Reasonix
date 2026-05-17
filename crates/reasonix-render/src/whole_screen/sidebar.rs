use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier};
use unicode_width::UnicodeWidthStr;

use crate::state::{SceneCard, SceneState, ToolStatus};

use super::cards::{parse_todo_items, wrap_visual, TodoState};
use super::paint::{paint, paint_str};
use super::theme::{BG, DS_BRIGHT, DS_PURPLE, ERR, FG, FG1, FG2, FG3, OK, WARN};

pub fn render_sidebar(buf: &mut Buffer, area: Rect, state: &SceneState) {
    for y in area.y..area.y + area.height {
        paint(buf, area.x, y, '│', FG3, BG, Modifier::empty());
    }

    let inner_x = area.x + 2;
    let mut row = area.y + 1;
    let bottom = area.y + area.height;

    if row < bottom {
        paint_str(buf, inner_x, row, "⚙ ", FG, BG, Modifier::empty());
        paint_str(
            buf,
            inner_x + 2,
            row,
            "MISSION CONTROL",
            DS_BRIGHT,
            BG,
            Modifier::BOLD,
        );
        let toggle = "^B hide";
        let toggle_col = area.x + area.width.saturating_sub(toggle.width() as u16 + 1);
        paint_str(buf, toggle_col, row, toggle, FG3, BG, Modifier::empty());
        row += 1;
    }
    row += 1;

    let plan_body = state
        .cards
        .iter()
        .rev()
        .find(|c| c.kind == "todo" || c.kind == "plan")
        .and_then(|c| c.body.as_deref())
        .unwrap_or("");
    let plan_items = parse_todo_items(plan_body);
    if plan_items.is_empty() {
        row = sidebar_section(
            buf,
            area,
            row,
            "◇",
            "PLAN",
            DS_PURPLE,
            "waiting for a task — type below",
        );
    } else {
        row = sidebar_plan(buf, area, row, &plan_items);
    }

    let recent_tools: Vec<&SceneCard> = state
        .cards
        .iter()
        .rev()
        .filter(|c| c.kind == "tool")
        .take(4)
        .collect();
    if recent_tools.is_empty() {
        row = sidebar_section(buf, area, row, "⚡", "JOBS", WARN, "no jobs run yet");
    } else {
        row = sidebar_jobs(buf, area, row, &recent_tools);
    }
    row = sidebar_section(buf, area, row, "▣", "CHANGES", DS_BRIGHT, "no edits yet");

    if row < bottom {
        paint_str(buf, inner_x, row, "▥ ", OK, BG, Modifier::BOLD);
        paint_str(buf, inner_x + 2, row, "SESSION", OK, BG, Modifier::BOLD);
        row += 1;
        let model_label = state.model.as_deref().unwrap_or("—");
        row = sidebar_kv(buf, area, row, "model", model_label, DS_BRIGHT);
        row = sidebar_kv(buf, area, row, "context", &format_ctx(state), FG);
        if let Some(n) = state.session_input_tokens {
            row = sidebar_kv(buf, area, row, "↑ input", &short_num(n), FG);
        }
        if let Some(n) = state.session_output_tokens {
            row = sidebar_kv(buf, area, row, "↓ output", &short_num(n), FG);
        }
        row = sidebar_kv(buf, area, row, "cache", &format_cache(state), FG);
        let currency = state.wallet_currency.as_deref();
        row = sidebar_kv(
            buf,
            area,
            row,
            "cost",
            &format_session_cost(state, currency),
            FG,
        );
        if let (Some(balance), Some(cur)) = (state.wallet_balance, currency) {
            row = sidebar_kv(
                buf,
                area,
                row,
                "balance",
                &format!("{:.2} {}", balance, cur),
                FG,
            );
        }
        let _ = sidebar_kv(
            buf,
            area,
            row,
            "last turn",
            &format_last_turn(state, currency),
            FG,
        );
    }
}

fn format_ctx(state: &SceneState) -> String {
    match (state.ctx_tokens, state.ctx_cap) {
        (Some(t), Some(c)) if c > 0 => format!("{} / {}", short_num(t), short_num(c)),
        _ => "—".to_string(),
    }
}

fn format_cache(state: &SceneState) -> String {
    state
        .cache_hit_ratio
        .map(|r| format!("{}%", (r * 100.0).round() as i32))
        .unwrap_or_else(|| "—".to_string())
}

fn format_session_cost(state: &SceneState, currency: Option<&str>) -> String {
    let Some(v) = state.session_cost_usd else {
        return "—".to_string();
    };
    match currency {
        Some("CNY") | Some("RMB") => format!("¥{:.3}", v * 7.2),
        _ => format!("${v:.3}"),
    }
}

fn format_last_turn(state: &SceneState, currency: Option<&str>) -> String {
    let elapsed = state.last_turn_ms.map(|ms| {
        if ms >= 60_000 {
            format!("{:.1}m", ms as f64 / 60_000.0)
        } else if ms >= 1000 {
            format!("{:.1}s", ms as f64 / 1000.0)
        } else {
            format!("{ms}ms")
        }
    });
    let cost = state.last_turn_cost_usd.map(|v| match currency {
        Some("CNY") | Some("RMB") => format!("¥{:.3}", v * 7.2),
        _ => format!("${v:.3}"),
    });
    match (elapsed, cost) {
        (Some(t), Some(c)) => format!("{t} · {c}"),
        (Some(t), None) => t,
        (None, Some(c)) => c,
        (None, None) => "—".to_string(),
    }
}

fn short_num(n: u32) -> String {
    if n >= 1_000_000 {
        format!("{:.1}M", f64::from(n) / 1_000_000.0)
    } else if n >= 100_000 {
        format!("{}k", n / 1000)
    } else if n >= 1_000 {
        format!("{:.1}k", f64::from(n) / 1000.0)
    } else {
        format!("{n}")
    }
}

fn sidebar_section(
    buf: &mut Buffer,
    area: Rect,
    start_row: u16,
    glyph: &str,
    title: &str,
    color: Color,
    empty_hint: &str,
) -> u16 {
    let bottom = area.y + area.height;
    let inner_x = area.x + 2;
    let mut row = start_row;
    if row >= bottom {
        return row;
    }
    paint_str(buf, inner_x, row, glyph, color, BG, Modifier::BOLD);
    paint_str(buf, inner_x + 2, row, title, color, BG, Modifier::BOLD);
    row += 1;
    if row < bottom {
        paint_str(buf, inner_x + 2, row, empty_hint, FG3, BG, Modifier::ITALIC);
        row += 1;
    }
    row + 1
}

fn sidebar_plan(buf: &mut Buffer, area: Rect, start_row: u16, items: &[(TodoState, &str)]) -> u16 {
    let bottom = area.y + area.height;
    let inner_x = area.x + 2;
    let mut row = start_row;
    if row >= bottom {
        return row;
    }
    let done = items
        .iter()
        .filter(|(s, _)| matches!(s, TodoState::Done))
        .count();
    paint_str(buf, inner_x, row, "◇", DS_PURPLE, BG, Modifier::BOLD);
    paint_str(buf, inner_x + 2, row, "PLAN", DS_PURPLE, BG, Modifier::BOLD);
    let count = format!("{}/{}", done, items.len());
    let ccol = area.x + area.width.saturating_sub(count.width() as u16 + 1);
    paint_str(buf, ccol, row, &count, FG3, BG, Modifier::empty());
    row += 1;
    let inner_w = area.width.saturating_sub(5);
    for (state, label) in items {
        if row >= bottom {
            return row;
        }
        let (marker, marker_fg, label_fg, label_mod) = match state {
            TodoState::Done => ("✓", OK, FG2, Modifier::empty()),
            TodoState::Active => ("◆", DS_BRIGHT, FG, Modifier::BOLD),
            TodoState::Pending => ("○", FG3, FG1, Modifier::empty()),
        };
        paint_str(buf, inner_x, row, marker, marker_fg, BG, Modifier::BOLD);
        for (i, seg) in wrap_visual(label, inner_w).iter().enumerate() {
            if row >= bottom {
                return row;
            }
            if i > 0 {
                paint(buf, area.x, row, '│', FG3, BG, Modifier::empty());
            }
            paint_str(buf, inner_x + 2, row, seg, label_fg, BG, label_mod);
            row += 1;
        }
    }
    row + 1
}

fn sidebar_jobs(buf: &mut Buffer, area: Rect, start_row: u16, tools: &[&SceneCard]) -> u16 {
    let bottom = area.y + area.height;
    let inner_x = area.x + 2;
    let mut row = start_row;
    if row >= bottom {
        return row;
    }
    paint_str(buf, inner_x, row, "⚡", WARN, BG, Modifier::BOLD);
    paint_str(buf, inner_x + 2, row, "JOBS", WARN, BG, Modifier::BOLD);
    let count = format!("{} recent", tools.len());
    let ccol = area.x + area.width.saturating_sub(count.width() as u16 + 1);
    paint_str(buf, ccol, row, &count, FG3, BG, Modifier::empty());
    row += 1;
    let inner_w = area.width.saturating_sub(5);
    for tool in tools {
        if row >= bottom {
            return row;
        }
        let (glyph, gfg) = match tool.status {
            Some(ToolStatus::Ok) => ("✓", OK),
            Some(ToolStatus::Err) => ("✕", ERR),
            Some(ToolStatus::Running) => ("…", WARN),
            None => ("·", FG3),
        };
        paint_str(buf, inner_x, row, glyph, gfg, BG, Modifier::BOLD);
        let mut label = tool.summary.clone();
        if let Some(args) = tool.args.as_deref() {
            label.push(' ');
            label.push_str(args);
        }
        for (i, seg) in wrap_visual(&label, inner_w).iter().enumerate() {
            if row >= bottom {
                return row;
            }
            if i > 0 {
                paint(buf, area.x, row, '│', FG3, BG, Modifier::empty());
            }
            paint_str(buf, inner_x + 2, row, seg, FG, BG, Modifier::empty());
            row += 1;
        }
    }
    row + 1
}

fn sidebar_kv(buf: &mut Buffer, area: Rect, row: u16, key: &str, val: &str, val_fg: Color) -> u16 {
    let bottom = area.y + area.height;
    if row >= bottom {
        return row;
    }
    let inner_x = area.x + 4;
    paint_str(buf, inner_x, row, key, FG2, BG, Modifier::empty());
    let val_col = area.x + area.width.saturating_sub(val.width() as u16 + 1);
    paint_str(buf, val_col, row, val, val_fg, BG, Modifier::empty());
    row + 1
}
