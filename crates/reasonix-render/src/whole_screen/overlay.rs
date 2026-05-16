use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::Modifier;
use unicode_width::UnicodeWidthStr;

use crate::state::SceneState;

use super::paint::{paint, paint_str};
use super::theme::{BG, DS, DS_BRIGHT, FG, FG2, FG3};

const SLASH_COMMANDS: &[(&str, &str, &str)] = &[
    ("/clear", "reset conversation context", "Ctrl+L"),
    ("/compact", "summarize history to free up tokens", "Ctrl+K"),
    ("/undo", "revert the last file edit", "⌘Z"),
    ("/commit", "create a git commit from current changes", ""),
    ("/diff", "show pending edits as a diff", ""),
    ("/help", "show help", "?"),
];

const MAX_ROWS: usize = 6;

pub fn slash_match_count(query: &str) -> usize {
    if !query.starts_with('/') {
        return 0;
    }
    SLASH_COMMANDS
        .iter()
        .filter(|(name, _, _)| name.starts_with(query))
        .count()
}

pub fn slash_completion(query: &str, idx: usize) -> Option<String> {
    if !query.starts_with('/') {
        return None;
    }
    SLASH_COMMANDS
        .iter()
        .filter(|(name, _, _)| name.starts_with(query))
        .nth(idx)
        .map(|(name, _, _)| format!("{name} "))
}

pub fn render_slash_overlay(
    buf: &mut Buffer,
    dock_area: Rect,
    state: &SceneState,
    selected_idx: usize,
) {
    let Some(text) = state.composer_text.as_deref() else {
        return;
    };
    if !text.starts_with('/') {
        return;
    }
    let matches = match_slash(text);
    if matches.is_empty() {
        return;
    }

    let visible = matches.len().min(MAX_ROWS) as u16;
    let popup_h = 3 + visible;
    let popup_w = dock_area.width.saturating_sub(4).min(70);
    if popup_w < 30 || popup_h > dock_area.y {
        return;
    }
    let popup_x = dock_area.x + 2;
    let popup_y = dock_area.y.saturating_sub(popup_h);
    let popup = Rect::new(popup_x, popup_y, popup_w, popup_h);

    let selected = selected_idx.min(matches.len().saturating_sub(1));

    draw_box(buf, popup);
    draw_header(buf, popup);
    draw_rows(buf, popup, &matches, text, selected);
}

fn match_slash(query: &str) -> Vec<&'static (&'static str, &'static str, &'static str)> {
    SLASH_COMMANDS
        .iter()
        .filter(|(name, _, _)| name.starts_with(query))
        .collect()
}

fn draw_box(buf: &mut Buffer, area: Rect) {
    let w = area.width;
    if w < 2 {
        return;
    }
    let top = area.y;
    let bot = area.y + area.height - 1;
    let right = area.x + w - 1;

    paint(buf, area.x, top, '╭', DS, BG, Modifier::empty());
    paint(buf, right, top, '╮', DS, BG, Modifier::empty());
    for x in 1..w - 1 {
        paint(buf, area.x + x, top, '─', DS, BG, Modifier::empty());
    }

    for y in (top + 1)..bot {
        paint(buf, area.x, y, '│', DS, BG, Modifier::empty());
        paint(buf, right, y, '│', DS, BG, Modifier::empty());
        for x in 1..w - 1 {
            paint(buf, area.x + x, y, ' ', FG, BG, Modifier::empty());
        }
    }

    paint(buf, area.x, bot, '╰', DS, BG, Modifier::empty());
    paint(buf, right, bot, '╯', DS, BG, Modifier::empty());
    for x in 1..w - 1 {
        paint(buf, area.x + x, bot, '─', DS, BG, Modifier::empty());
    }
}

fn draw_header(buf: &mut Buffer, area: Rect) {
    let row = area.y + 1;
    paint_str(buf, area.x + 2, row, "/ SLASH COMMANDS", DS_BRIGHT, BG, Modifier::BOLD);
    let hint = "↑↓/Tab navigate  ↵ select  esc dismiss";
    let hcol = area.x + area.width.saturating_sub(hint.width() as u16 + 2);
    paint_str(buf, hcol, row, hint, FG2, BG, Modifier::empty());
}

fn draw_rows(
    buf: &mut Buffer,
    area: Rect,
    matches: &[&'static (&'static str, &'static str, &'static str)],
    query: &str,
    selected_idx: usize,
) {
    let body_top = area.y + 2;
    let max_name_w = matches.iter().map(|(n, _, _)| n.width()).max().unwrap_or(8) as u16;
    let name_col = area.x + 4;
    let desc_col = name_col + max_name_w + 2;

    for (i, (name, desc, hot)) in matches.iter().enumerate().take(MAX_ROWS) {
        let row = body_top + i as u16;
        let selected = i == selected_idx;

        if selected {
            paint_str(buf, area.x + 2, row, "▸", DS_BRIGHT, BG, Modifier::BOLD);
            paint_str(buf, name_col, row, name, FG, BG, Modifier::BOLD);
        } else {
            paint_str(buf, name_col, row, name, DS_BRIGHT, BG, Modifier::BOLD);
        }

        if let Some(suffix) = name.strip_prefix(query) {
            let typed_w = query.width() as u16;
            paint_str(
                buf,
                name_col + typed_w,
                row,
                suffix,
                FG2,
                BG,
                Modifier::empty(),
            );
        }

        let desc_fg = if selected { FG } else { FG2 };
        paint_str(buf, desc_col, row, desc, desc_fg, BG, Modifier::empty());

        if !hot.is_empty() {
            let hot_col = area.x + area.width.saturating_sub(hot.width() as u16 + 2);
            paint_str(buf, hot_col, row, hot, FG3, BG, Modifier::empty());
        }
    }
}
