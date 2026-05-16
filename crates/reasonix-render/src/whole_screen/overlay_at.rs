use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::Modifier;
use unicode_width::UnicodeWidthStr;

use crate::state::SceneState;

use super::paint::{paint, paint_str};
use super::theme::{BG, DS_PURPLE, FG, FG2, WARN};

const AT_FILES: &[&str] = &[
    "src/parser.ts",
    "src/utils/format.ts",
    "src/utils/escape.ts",
    "src/index.ts",
    "src/cli/ui/App.tsx",
    "tests/parser.test.ts",
    "tests/format.test.ts",
    "README.md",
    "CLAUDE.md",
    "package.json",
];

const MAX_ROWS: usize = 6;

fn at_token(buffer: &str) -> Option<(usize, &str)> {
    let at_pos = buffer.rfind('@')?;
    let prefix = &buffer[..at_pos];
    let suffix = &buffer[at_pos + 1..];
    let valid_prefix = prefix.is_empty()
        || prefix.chars().last().is_some_and(char::is_whitespace);
    if !valid_prefix {
        return None;
    }
    if suffix.chars().any(char::is_whitespace) {
        return None;
    }
    Some((at_pos, suffix))
}

pub fn at_match_count(buffer: &str) -> usize {
    let Some((_, query)) = at_token(buffer) else {
        return 0;
    };
    AT_FILES.iter().filter(|f| f.contains(query)).count()
}

pub fn at_completion(buffer: &str, idx: usize) -> Option<String> {
    let (at_pos, query) = at_token(buffer)?;
    let chosen = AT_FILES.iter().filter(|f| f.contains(query)).nth(idx)?;
    let prefix = &buffer[..at_pos];
    Some(format!("{prefix}@{chosen} "))
}

pub fn render_at_overlay(
    buf: &mut Buffer,
    dock_area: Rect,
    state: &SceneState,
    selected_idx: usize,
) {
    let Some(text) = state.composer_text.as_deref() else {
        return;
    };
    let Some((_, query)) = at_token(text) else {
        return;
    };
    let matches: Vec<&&str> = AT_FILES.iter().filter(|f| f.contains(query)).collect();
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
    draw_header(buf, popup, matches.len());
    draw_rows(buf, popup, &matches, query, selected);
}

fn draw_box(buf: &mut Buffer, area: Rect) {
    let w = area.width;
    if w < 2 {
        return;
    }
    let top = area.y;
    let bot = area.y + area.height - 1;
    let right = area.x + w - 1;
    paint(buf, area.x, top, '╭', DS_PURPLE, BG, Modifier::empty());
    paint(buf, right, top, '╮', DS_PURPLE, BG, Modifier::empty());
    for x in 1..w - 1 {
        paint(buf, area.x + x, top, '─', DS_PURPLE, BG, Modifier::empty());
    }
    for y in (top + 1)..bot {
        paint(buf, area.x, y, '│', DS_PURPLE, BG, Modifier::empty());
        paint(buf, right, y, '│', DS_PURPLE, BG, Modifier::empty());
        for x in 1..w - 1 {
            paint(buf, area.x + x, y, ' ', FG, BG, Modifier::empty());
        }
    }
    paint(buf, area.x, bot, '╰', DS_PURPLE, BG, Modifier::empty());
    paint(buf, right, bot, '╯', DS_PURPLE, BG, Modifier::empty());
    for x in 1..w - 1 {
        paint(buf, area.x + x, bot, '─', DS_PURPLE, BG, Modifier::empty());
    }
}

fn draw_header(buf: &mut Buffer, area: Rect, count: usize) {
    let row = area.y + 1;
    paint_str(buf, area.x + 2, row, "@ ATTACH FILE", DS_PURPLE, BG, Modifier::BOLD);
    let hint = format!("{count} matches  ↑↓/Tab navigate  ↵ select  esc dismiss");
    let hcol = area.x + area.width.saturating_sub(hint.width() as u16 + 2);
    paint_str(buf, hcol, row, &hint, FG2, BG, Modifier::empty());
}

fn draw_rows(
    buf: &mut Buffer,
    area: Rect,
    matches: &[&&'static str],
    query: &str,
    selected_idx: usize,
) {
    let body_top = area.y + 2;
    let name_col = area.x + 4;
    let max_path_w = area.width.saturating_sub(6);

    for (i, path) in matches.iter().enumerate().take(MAX_ROWS) {
        let row = body_top + i as u16;
        let selected = i == selected_idx;
        if selected {
            paint_str(buf, area.x + 2, row, "▸", DS_PURPLE, BG, Modifier::BOLD);
        }
        paint_path_with_highlight(buf, name_col, row, path, query, max_path_w, selected);
    }
}

fn paint_path_with_highlight(
    buf: &mut Buffer,
    x: u16,
    row: u16,
    path: &str,
    query: &str,
    max_w: u16,
    selected: bool,
) {
    let base_fg = if selected { FG } else { FG2 };
    let base_mod = if selected { Modifier::BOLD } else { Modifier::empty() };
    if query.is_empty() {
        paint_clipped(buf, x, row, path, base_fg, base_mod, max_w);
        return;
    }
    if let Some(pos) = path.find(query) {
        let before = &path[..pos];
        let mid = &path[pos..pos + query.len()];
        let after = &path[pos + query.len()..];
        let mut col = x;
        col = paint_clipped(buf, col, row, before, base_fg, base_mod, x + max_w - col);
        col = paint_clipped(buf, col, row, mid, WARN, Modifier::BOLD, x + max_w - col);
        let _ = paint_clipped(buf, col, row, after, base_fg, base_mod, x + max_w - col);
    } else {
        paint_clipped(buf, x, row, path, base_fg, base_mod, max_w);
    }
}

fn paint_clipped(
    buf: &mut Buffer,
    x: u16,
    row: u16,
    s: &str,
    fg: ratatui::style::Color,
    modifier: Modifier,
    max_w: u16,
) -> u16 {
    if max_w == 0 {
        return x;
    }
    let mut w = 0u16;
    let mut clipped = String::new();
    for ch in s.chars() {
        let cw = unicode_width::UnicodeWidthChar::width(ch).unwrap_or(1) as u16;
        if w + cw > max_w {
            break;
        }
        w += cw;
        clipped.push(ch);
    }
    paint_str(buf, x, row, &clipped, fg, BG, modifier);
    x + w
}

