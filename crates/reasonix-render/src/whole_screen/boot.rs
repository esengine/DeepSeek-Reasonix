use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::Modifier;

use crate::state::SceneState;

use super::cards::render_cards;
use super::paint::{paint_link, paint_str_to};
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
    let end_x = area.x.saturating_add(area.width);
    let mut row = start_row;
    for line in LOGO {
        if row >= area.y + area.height {
            break;
        }
        paint_str_to(buf, area.x + 2, row, line, end_x, DS, BG, Modifier::BOLD);
        row += 1;
    }
    row
}

fn render_boot_meta(buf: &mut Buffer, area: Rect, start_row: u16, state: &SceneState) -> u16 {
    use super::theme::{BG, DS_BRIGHT};
    let bottom = area.y + area.height;
    let end_x = area.x.saturating_add(area.width);
    let key_col = area.x + 2;
    let val_col = area.x + 14;
    let mut row = start_row;

    if row < bottom {
        if let Some(model) = state.model.as_deref() {
            paint_str_to(
                buf,
                key_col,
                row,
                "model",
                end_x,
                FG2,
                BG,
                Modifier::empty(),
            );
            let after = paint_str_to(
                buf,
                val_col,
                row,
                model,
                end_x,
                DS_BRIGHT,
                BG,
                Modifier::empty(),
            );
            if let Some(cap) = state.ctx_cap {
                let ctx_col = after.saturating_add(4);
                paint_str_to(
                    buf,
                    ctx_col,
                    row,
                    "context",
                    end_x,
                    FG2,
                    BG,
                    Modifier::empty(),
                );
                let ctx_text = match (state.ctx_tokens, cap) {
                    (Some(used), cap) if cap > 0 => {
                        let pct = (f64::from(used) / f64::from(cap) * 100.0) as i32;
                        format!("{} · {pct}% used", short_tokens(cap))
                    }
                    _ => short_tokens(cap),
                };
                paint_str_to(
                    buf,
                    ctx_col.saturating_add(10),
                    row,
                    &ctx_text,
                    end_x,
                    FG,
                    BG,
                    Modifier::empty(),
                );
            }
            row += 1;
        }
    }

    if row < bottom {
        if let Some(cwd) = state.cwd.as_deref() {
            paint_str_to(
                buf,
                key_col,
                row,
                "workdir",
                end_x,
                FG2,
                BG,
                Modifier::empty(),
            );
            paint_str_to(buf, val_col, row, cwd, end_x, FG, BG, Modifier::empty());
            row += 1;
        }
    }

    if row < bottom {
        if let Some(url) = state.dashboard_url.as_deref() {
            paint_str_to(
                buf,
                key_col,
                row,
                "dashboard",
                end_x,
                FG2,
                BG,
                Modifier::empty(),
            );
            paint_link(buf, val_col, row, url, url, DS_BRIGHT, BG);
            row += 1;
        }
    }

    row
}

fn short_tokens(n: u32) -> String {
    if n >= 1_000_000 {
        format!("{:.1}M", f64::from(n) / 1_000_000.0)
    } else if n >= 1_000 {
        format!("{}k", n / 1000)
    } else {
        format!("{n}")
    }
}

fn render_hint_line(buf: &mut Buffer, area: Rect, row: u16) -> u16 {
    use super::theme::BG;
    if row >= area.y + area.height {
        return row;
    }
    let end_x = area.x.saturating_add(area.width);
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
        if col >= end_x {
            break;
        }
        if i > 0 {
            col = paint_str_to(buf, col, row, "  ·  ", end_x, FG3, BG, Modifier::empty());
        }
        if label.is_empty() {
            col = paint_str_to(buf, col, row, key, end_x, FG2, BG, Modifier::empty());
        } else {
            col = paint_str_to(buf, col, row, key, end_x, DS, BG, Modifier::BOLD);
            col = paint_str_to(buf, col, row, " ", end_x, FG2, BG, Modifier::empty());
            col = paint_str_to(buf, col, row, label, end_x, FG2, BG, Modifier::empty());
        }
    }
    row + 1
}
