use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::Modifier;

use crate::state::SceneState;

use super::cards::render_cards;
use super::paint::paint_str;
use super::theme::{DS, FG, FG2, FG3, LOGO};

pub fn render_scroll(
    buf: &mut Buffer,
    area: Rect,
    state: &SceneState,
    scroll_offset: u16,
    tick: u32,
) {
    if area.height == 0 {
        return;
    }
    let mut row = area.y + 1;
    row = render_logo(buf, area, row);
    row = row.saturating_add(1);
    row = render_boot_meta(buf, area, row, state);
    row = row.saturating_add(1);
    row = render_hint_line(buf, area, row);
    row = row.saturating_add(1);
    render_cards(buf, area, row, state, scroll_offset, tick);
}

fn render_logo(buf: &mut Buffer, area: Rect, start_row: u16) -> u16 {
    use super::theme::BG;
    let mut row = start_row;
    for line in LOGO {
        if row >= area.y + area.height {
            break;
        }
        paint_str(buf, area.x + 2, row, line, DS, BG, Modifier::BOLD);
        row += 1;
    }
    row
}

fn render_boot_meta(buf: &mut Buffer, area: Rect, start_row: u16, state: &SceneState) -> u16 {
    use super::theme::{BG, DS_BRIGHT};
    let bottom = area.y + area.height;
    let key_col = area.x + 2;
    let val_col = area.x + 14;
    let mut row = start_row;

    let model = state.model.as_deref().unwrap_or("deepseek-v3.2-coder");
    if row < bottom {
        paint_str(buf, key_col, row, "model", FG2, BG, Modifier::empty());
        let after = paint_str(buf, val_col, row, model, DS_BRIGHT, BG, Modifier::empty());
        let ctx_col = after.saturating_add(4);
        paint_str(buf, ctx_col, row, "context", FG2, BG, Modifier::empty());
        paint_str(
            buf,
            ctx_col.saturating_add(10),
            row,
            "128k · 12% used",
            FG,
            BG,
            Modifier::empty(),
        );
        row += 1;
    }

    if row < bottom {
        paint_str(buf, key_col, row, "workdir", FG2, BG, Modifier::empty());
        let cwd = state.cwd.as_deref().unwrap_or("~/work/reasonix-core");
        paint_str(buf, val_col, row, cwd, FG, BG, Modifier::empty());
        row += 1;
    }

    if row < bottom {
        paint_str(buf, key_col, row, "git", FG2, BG, Modifier::empty());
        paint_str(buf, val_col, row, "main · 3 modified · ↑2", FG, BG, Modifier::empty());
        row += 1;
    }

    if row < bottom {
        paint_str(buf, key_col, row, "tools", FG2, BG, Modifier::empty());
        paint_str(
            buf,
            val_col,
            row,
            "read · write · edit · bash · grep · fetch · todo",
            FG,
            BG,
            Modifier::empty(),
        );
        row += 1;
    }
    row
}

fn render_hint_line(buf: &mut Buffer, area: Rect, row: u16) -> u16 {
    use super::theme::BG;
    if row >= area.y + area.height {
        return row;
    }
    let mut col = area.x + 2;
    let pairs: [(&str, &str); 6] = [
        ("type to chat", ""),
        ("/", "commands"),
        ("@", "file refs"),
        ("!", "shell"),
        ("Ctrl+C", "cancel"),
        ("Ctrl+D", "exit"),
    ];
    for (i, (key, label)) in pairs.iter().enumerate() {
        if i > 0 {
            col = paint_str(buf, col, row, "  ·  ", FG3, BG, Modifier::empty());
        }
        if label.is_empty() {
            col = paint_str(buf, col, row, key, FG2, BG, Modifier::empty());
        } else {
            col = paint_str(buf, col, row, key, DS, BG, Modifier::BOLD);
            col = paint_str(buf, col, row, " ", FG2, BG, Modifier::empty());
            col = paint_str(buf, col, row, label, FG2, BG, Modifier::empty());
        }
    }
    row + 1
}
