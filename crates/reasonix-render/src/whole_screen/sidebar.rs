use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier};
use unicode_width::UnicodeWidthStr;

use crate::state::{SceneCard, SceneState, ToolStatus};

use super::cards::{parse_todo_items, TodoState};
use super::paint::{paint, paint_str, truncate};
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
        paint_str(buf, inner_x + 2, row, "MISSION CONTROL", DS_BRIGHT, BG, Modifier::BOLD);
        let toggle = "⌘. toggle";
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
        row = sidebar_section(buf, area, row, "◇", "PLAN", DS_PURPLE, "waiting for a task — type below");
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
        row = sidebar_kv(buf, area, row, "model", "v3.2-coder", DS_BRIGHT);
        row = sidebar_kv(buf, area, row, "context", "19.2k / 128k", FG);
        row = sidebar_kv(buf, area, row, "↑ input", "12,408", FG);
        row = sidebar_kv(buf, area, row, "↓ output", "3,194", FG);
        row = sidebar_kv(buf, area, row, "cost", "$0.043", FG);
        let _ = sidebar_kv(buf, area, row, "last turn", "—", FG);
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
    let done = items.iter().filter(|(s, _)| matches!(s, TodoState::Done)).count();
    paint_str(buf, inner_x, row, "◇", DS_PURPLE, BG, Modifier::BOLD);
    paint_str(buf, inner_x + 2, row, "PLAN", DS_PURPLE, BG, Modifier::BOLD);
    let count = format!("{}/{}", done, items.len());
    let ccol = area.x + area.width.saturating_sub(count.width() as u16 + 1);
    paint_str(buf, ccol, row, &count, FG3, BG, Modifier::empty());
    row += 1;
    let inner_w = area.width.saturating_sub(3);
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
        paint_str(buf, inner_x + 2, row, &truncate(label, inner_w as usize), label_fg, BG, label_mod);
        row += 1;
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
    let inner_w = area.width.saturating_sub(3);
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
        paint_str(buf, inner_x + 2, row, &truncate(&label, inner_w as usize), FG, BG, Modifier::empty());
        row += 1;
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
