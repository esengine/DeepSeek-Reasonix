use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier};
use unicode_width::UnicodeWidthStr;

use crate::state::{SceneCard, ToolStatus};

use super::super::paint::paint_str;
use super::super::theme::{BG, DS_BRIGHT, DS_PURPLE, ERR, FG, FG1, FG2, FG3, INFO, OK, WARN};
use super::paint_rail;

const SPINNER: [char; 10] = ['⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'];

fn spinner_frame(tick: u32) -> char {
    SPINNER[(tick as usize) % SPINNER.len()]
}

pub(super) fn render_tool_card(
    buf: &mut Buffer,
    area: Rect,
    row: u16,
    card: &SceneCard,
    tick: u32,
) -> u16 {
    let bottom = area.y + area.height;
    if row >= bottom {
        return row;
    }
    paint_rail(buf, area, row, FG2);
    let mut col = area.x + 2;
    col = paint_str(buf, col, row, "▸ ", FG2, BG, Modifier::empty());
    let name = card.summary.as_str();
    let name_fg = tool_name_color(name);
    col = paint_str(buf, col, row, name, name_fg, BG, Modifier::BOLD);
    if let Some(args) = card.args.as_deref() {
        col = paint_str(buf, col, row, " (", FG2, BG, Modifier::empty());
        col = paint_str(buf, col, row, args, FG1, BG, Modifier::empty());
        col = paint_str(buf, col, row, ")", FG2, BG, Modifier::empty());
    }
    let elapsed = card.elapsed.as_deref().unwrap_or("");
    let id = card.id.as_deref().unwrap_or("");
    let mut spin_buf = [0u8; 4];
    let (status_glyph, status_fg): (&str, _) = match card.status {
        Some(ToolStatus::Ok) => ("✓", OK),
        Some(ToolStatus::Err) => ("✕", ERR),
        Some(ToolStatus::Running) => (spinner_frame(tick).encode_utf8(&mut spin_buf), WARN),
        None => ("", FG3),
    };
    let right_w =
        status_glyph.width() as u16 + 1 + elapsed.width() as u16 + 2 + id.width() as u16 + 1;
    let mut rcol = area.x + area.width.saturating_sub(right_w);
    if rcol < col + 2 {
        rcol = col + 2;
    }
    if !status_glyph.is_empty() {
        rcol = paint_str(buf, rcol, row, status_glyph, status_fg, BG, Modifier::BOLD);
        rcol = rcol.saturating_add(1);
    }
    if !elapsed.is_empty() {
        rcol = paint_str(buf, rcol, row, elapsed, FG2, BG, Modifier::empty());
        rcol = rcol.saturating_add(2);
    }
    if !id.is_empty() {
        paint_str(buf, rcol, row, id, FG3, BG, Modifier::empty());
    }
    row + 1
}

fn tool_name_color(name: &str) -> Color {
    match name {
        "Read" | "FileView" => DS_BRIGHT,
        "Grep" | "Search" => INFO,
        "Edit" | "Write" => DS_PURPLE,
        "Bash" => OK,
        "Fetch" | "WebFetch" => WARN,
        _ => FG,
    }
}
