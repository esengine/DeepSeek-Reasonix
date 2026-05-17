use chrono::{Local, TimeZone};
use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier, Style};
use unicode_width::UnicodeWidthChar;

/// Display width of a character.  Returns 0 for control characters
/// (where unicode-width returns None) and combining marks, so the
/// layout engine skips them instead of giving them a spurious width-1
/// cell that overwrites the next character.
pub fn char_width(ch: char) -> u16 {
    UnicodeWidthChar::width(ch).unwrap_or(0) as u16
}

pub fn paint(buf: &mut Buffer, x: u16, y: u16, ch: char, fg: Color, bg: Color, modifier: Modifier) {
    if x >= buf.area.right() || y >= buf.area.bottom() {
        return;
    }
    let w = char_width(ch);
    if w == 0 {
        return;
    }
    let mut tmp = [0u8; 4];
    let s = ch.encode_utf8(&mut tmp);
    let style = Style::default().fg(fg).bg(bg).add_modifier(modifier);
    buf[(x, y)].set_symbol(s).set_style(style).set_skip(false);
    for off in 1..w {
        let cx = x + off;
        if cx >= buf.area.right() {
            break;
        }
        buf[(cx, y)]
            .set_symbol("")
            .set_style(Style::default().bg(bg))
            .set_skip(true);
    }
}

pub fn paint_str(
    buf: &mut Buffer,
    x: u16,
    y: u16,
    s: &str,
    fg: Color,
    bg: Color,
    modifier: Modifier,
) -> u16 {
    paint_str_to(buf, x, y, s, buf.area.right(), fg, bg, modifier)
}

#[allow(clippy::too_many_arguments)]
pub fn paint_str_to(
    buf: &mut Buffer,
    x: u16,
    y: u16,
    s: &str,
    end_x: u16,
    fg: Color,
    bg: Color,
    modifier: Modifier,
) -> u16 {
    let limit = end_x.min(buf.area.right());
    let mut col = x;
    for ch in s.chars() {
        // Tab → 2 spaces (terminal convention, matches common tab-stop width).
        if ch == '\t' {
            for _ in 0..2 {
                if col >= limit {
                    break;
                }
                paint(buf, col, y, ' ', fg, bg, modifier);
                col += 1;
            }
            continue;
        }
        let w = char_width(ch);
        // Zero-width characters (control chars, combining marks, ZWJ/ZWNJ,
        // variation selectors) are skipped — they'd otherwise land at the
        // same column as the next character and cause overwrite artifacts.
        if w == 0 {
            continue;
        }
        if col.saturating_add(w) > limit {
            break;
        }
        paint(buf, col, y, ch, fg, bg, modifier);
        col = col.saturating_add(w);
    }
    col
}

/// OSC 8 hyperlink — the entire escape sequence (open + visible text +
/// close) lives in one cell's symbol; subsequent cells covered by the
/// visible width are flagged `skip` so the diff layer doesn't repaint
/// them. Terminals that understand OSC 8 (Windows Terminal, iTerm2,
/// WezTerm, gnome-terminal ≥3.26, kitty, etc.) render this as a
/// clickable link; older terminals print the escapes as no-ops and
/// fall back to plain underlined text.
pub fn paint_link(
    buf: &mut Buffer,
    x: u16,
    y: u16,
    url: &str,
    visible: &str,
    fg: Color,
    bg: Color,
) -> u16 {
    use ratatui::style::Style;
    use unicode_width::UnicodeWidthStr;
    if x >= buf.area.right() || y >= buf.area.bottom() {
        return x;
    }
    let width = UnicodeWidthStr::width(visible) as u16;
    if width == 0 {
        return x;
    }
    let symbol = format!("\x1b]8;;{url}\x1b\\{visible}\x1b]8;;\x1b\\");
    let style = Style::default()
        .fg(fg)
        .bg(bg)
        .add_modifier(Modifier::UNDERLINED);
    buf[(x, y)]
        .set_symbol(&symbol)
        .set_style(style)
        .set_skip(false);
    for off in 1..width {
        let cx = x + off;
        if cx >= buf.area.right() {
            break;
        }
        buf[(cx, y)]
            .set_symbol("")
            .set_style(Style::default().bg(bg))
            .set_skip(true);
    }
    x + width
}

/// Multi-row variant of `paint_link`. Splits `visible` into chunks that fit
/// `[x, end_x)` and stacks them on consecutive rows, each chunk a clickable
/// link to the full `url`. Returns rows consumed (0 if nothing fit).
#[allow(clippy::too_many_arguments)]
pub fn paint_link_wrapped(
    buf: &mut Buffer,
    x: u16,
    y: u16,
    end_x: u16,
    bottom: u16,
    url: &str,
    visible: &str,
    fg: Color,
    bg: Color,
) -> u16 {
    use unicode_width::UnicodeWidthChar;
    let limit = end_x.min(buf.area.right());
    let bottom = bottom.min(buf.area.bottom());
    if x >= limit || y >= bottom {
        return 0;
    }
    let max_w = (limit - x) as usize;
    if max_w == 0 {
        return 0;
    }
    let mut row = y;
    let mut chunk = String::new();
    let mut chunk_w = 0usize;
    for ch in visible.chars() {
        let cw = UnicodeWidthChar::width(ch).unwrap_or(1);
        if chunk_w + cw > max_w && !chunk.is_empty() {
            paint_link(buf, x, row, url, &chunk, fg, bg);
            row += 1;
            if row >= bottom {
                return row - y;
            }
            chunk.clear();
            chunk_w = 0;
        }
        chunk.push(ch);
        chunk_w += cw;
    }
    if !chunk.is_empty() {
        paint_link(buf, x, row, url, &chunk, fg, bg);
        row += 1;
    }
    row - y
}

pub fn fill_bg(buf: &mut Buffer, area: Rect, bg: Color) {
    let style = Style::default().bg(bg);
    for y in area.y..area.y.saturating_add(area.height) {
        for x in area.x..area.x.saturating_add(area.width) {
            buf[(x, y)].set_style(style);
        }
    }
}

pub fn format_ts(ts: i64) -> String {
    Local
        .timestamp_opt(ts, 0)
        .single()
        .map(|dt| dt.format("%H:%M:%S").to_string())
        .unwrap_or_default()
}
