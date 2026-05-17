use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier};
use unicode_width::UnicodeWidthChar;

use super::cards::{body_indent_col, paint_rail};
use super::markdown::{parse, CellAlign, InlineSpan, InlineStyle, MdBlock};
use super::paint::paint_str;
use super::theme::{BG, DS_BRIGHT, FG2, FG3, INFO, OK, WARN};

pub fn count_visual_rows(text: &str, width: u16) -> usize {
    let blocks = parse(text);
    let mut total = 0usize;
    for block in &blocks {
        total += block_rows(block, width);
    }
    total
}

pub fn render_markdown(
    buf: &mut Buffer,
    area: Rect,
    start_row: u16,
    bottom: u16,
    rail_color: Color,
    default_fg: Color,
    text: &str,
) -> u16 {
    let blocks = parse(text);
    let mut row = start_row;
    let width = area.width.saturating_sub(4);
    for block in blocks {
        if row >= bottom {
            return row;
        }
        row = render_block(
            buf, area, row, bottom, rail_color, default_fg, &block, width,
        );
    }
    row
}

#[allow(clippy::too_many_arguments)]
fn render_block(
    buf: &mut Buffer,
    area: Rect,
    mut row: u16,
    bottom: u16,
    rail_color: Color,
    default_fg: Color,
    block: &MdBlock,
    width: u16,
) -> u16 {
    match block {
        MdBlock::Paragraph(spans) => {
            let lines = wrap_spans(spans, width);
            for line in lines {
                if row >= bottom {
                    return row;
                }
                paint_rail(buf, area, row, rail_color);
                paint_styled(buf, body_indent_col(area), row, &line, default_fg);
                row += 1;
            }
        }
        MdBlock::Heading { level, spans } => {
            if row >= bottom {
                return row;
            }
            paint_rail(buf, area, row, rail_color);
            let prefix = match level {
                1 => "▎ ",
                2 => "▍ ",
                _ => "▏ ",
            };
            let mut col = body_indent_col(area);
            col = paint_str(buf, col, row, prefix, DS_BRIGHT, BG, Modifier::BOLD);
            let bold_spans: Vec<InlineSpan> = spans
                .iter()
                .map(|s| {
                    let mut style = s.style.clone();
                    style.bold = true;
                    InlineSpan {
                        text: s.text.clone(),
                        style,
                    }
                })
                .collect();
            paint_styled(buf, col, row, &bold_spans, DS_BRIGHT);
            row += 1;
        }
        MdBlock::Code { lang: _, text } => {
            for line in text.lines() {
                if row >= bottom {
                    return row;
                }
                paint_rail(buf, area, row, rail_color);
                let col = body_indent_col(area);
                paint_str(buf, col, row, "┃ ", FG3, BG, Modifier::empty());
                paint_str(buf, col + 2, row, line, OK, BG, Modifier::empty());
                row += 1;
            }
        }
        MdBlock::ListItem {
            ordered,
            index,
            depth,
            spans,
        } => {
            let indent = " ".repeat(*depth as usize * 2);
            let marker = if *ordered {
                format!("{indent}{index}. ")
            } else {
                format!("{indent}• ")
            };
            let marker_w = marker
                .chars()
                .map(|c| UnicodeWidthChar::width(c).unwrap_or(0))
                .sum::<usize>() as u16;
            let body_w = width.saturating_sub(marker_w);
            let lines = wrap_spans(spans, body_w);
            for (i, line) in lines.iter().enumerate() {
                if row >= bottom {
                    return row;
                }
                paint_rail(buf, area, row, rail_color);
                let mut col = body_indent_col(area);
                if i == 0 {
                    col = paint_str(buf, col, row, &marker, FG2, BG, Modifier::BOLD);
                } else {
                    col = paint_str(
                        buf,
                        col,
                        row,
                        &" ".repeat(marker_w as usize),
                        default_fg,
                        BG,
                        Modifier::empty(),
                    );
                }
                paint_styled(buf, col, row, line, default_fg);
                row += 1;
            }
        }
        MdBlock::BlockQuote(spans) => {
            let lines = wrap_spans(spans, width.saturating_sub(2));
            for line in lines {
                if row >= bottom {
                    return row;
                }
                paint_rail(buf, area, row, rail_color);
                let col = body_indent_col(area);
                paint_str(buf, col, row, "▌ ", FG3, BG, Modifier::empty());
                paint_styled(buf, col + 2, row, &line, FG2);
                row += 1;
            }
        }
        MdBlock::Hr => {
            if row >= bottom {
                return row;
            }
            paint_rail(buf, area, row, rail_color);
            let col = body_indent_col(area);
            let dash: String = "─".repeat(width as usize);
            paint_str(buf, col, row, &dash, FG3, BG, Modifier::empty());
            row += 1;
        }
        MdBlock::Table { aligns, head, rows } => {
            row = render_table(
                buf, area, row, bottom, rail_color, default_fg, width, aligns, head, rows,
            );
        }
    }
    row
}

#[allow(clippy::too_many_arguments)]
fn render_table(
    buf: &mut Buffer,
    area: Rect,
    mut row: u16,
    bottom: u16,
    rail_color: Color,
    default_fg: Color,
    width: u16,
    aligns: &[CellAlign],
    head: &[Vec<InlineSpan>],
    rows: &[Vec<Vec<InlineSpan>>],
) -> u16 {
    let n_cols = head
        .len()
        .max(rows.iter().map(|r| r.len()).max().unwrap_or(0));
    if n_cols == 0 {
        return row;
    }
    let mut col_widths = vec![0usize; n_cols];
    for (i, cell) in head.iter().enumerate() {
        if i < n_cols {
            col_widths[i] = col_widths[i].max(span_text_width(cell));
        }
    }
    for r in rows {
        for (i, cell) in r.iter().enumerate() {
            if i < n_cols {
                col_widths[i] = col_widths[i].max(span_text_width(cell));
            }
        }
    }
    let frame_cols = n_cols * 3 + 1;
    let total_min: usize = col_widths.iter().sum::<usize>() + frame_cols;
    let avail = width as usize;
    if total_min > avail {
        let overflow = total_min - avail;
        let mut remaining = overflow;
        while remaining > 0 {
            let max_idx = col_widths
                .iter()
                .enumerate()
                .max_by_key(|(_, w)| **w)
                .map(|(i, _)| i)
                .unwrap_or(0);
            if col_widths[max_idx] <= 3 {
                break;
            }
            col_widths[max_idx] -= 1;
            remaining -= 1;
        }
    }
    let base = body_indent_col(area);

    let draw_border = |buf: &mut Buffer, y: u16, l: char, m: char, r: char, h: char| {
        paint_rail(buf, area, y, rail_color);
        let mut c = base;
        let lc = l.to_string();
        c = paint_str(buf, c, y, &lc, FG3, BG, Modifier::empty());
        for (i, cw) in col_widths.iter().enumerate() {
            let bar: String = (0..*cw + 2).map(|_| h).collect();
            c = paint_str(buf, c, y, &bar, FG3, BG, Modifier::empty());
            let sep = if i + 1 == col_widths.len() {
                r.to_string()
            } else {
                m.to_string()
            };
            c = paint_str(buf, c, y, &sep, FG3, BG, Modifier::empty());
        }
        let _ = c;
    };

    if row < bottom {
        draw_border(buf, row, '╭', '┬', '╮', '─');
        row += 1;
    }
    if !head.is_empty() && row < bottom {
        paint_rail(buf, area, row, rail_color);
        let mut c = base;
        c = paint_str(buf, c, row, "│", FG3, BG, Modifier::empty());
        for (i, cell) in head.iter().enumerate() {
            if i >= n_cols {
                break;
            }
            c = paint_str(buf, c, row, " ", default_fg, BG, Modifier::empty());
            let align = aligns.get(i).copied().unwrap_or(CellAlign::Left);
            c = paint_cell(buf, c, row, cell, col_widths[i], align, default_fg, true);
            c = paint_str(buf, c, row, " │", FG3, BG, Modifier::empty());
        }
        let _ = c;
        row += 1;
        if row < bottom {
            draw_border(buf, row, '├', '┼', '┤', '─');
            row += 1;
        }
    }
    for r in rows {
        if row >= bottom {
            break;
        }
        paint_rail(buf, area, row, rail_color);
        let mut c = base;
        c = paint_str(buf, c, row, "│", FG3, BG, Modifier::empty());
        for (i, &col_w) in col_widths.iter().enumerate().take(n_cols) {
            c = paint_str(buf, c, row, " ", default_fg, BG, Modifier::empty());
            let empty: Vec<InlineSpan> = Vec::new();
            let cell = r.get(i).unwrap_or(&empty);
            let align = aligns.get(i).copied().unwrap_or(CellAlign::Left);
            c = paint_cell(buf, c, row, cell, col_w, align, default_fg, false);
            c = paint_str(buf, c, row, " │", FG3, BG, Modifier::empty());
        }
        let _ = c;
        row += 1;
    }
    if row < bottom {
        draw_border(buf, row, '╰', '┴', '╯', '─');
        row += 1;
    }
    row
}

fn span_text_width(spans: &[InlineSpan]) -> usize {
    spans
        .iter()
        .flat_map(|s| s.text.chars())
        .map(|c| UnicodeWidthChar::width(c).unwrap_or(0))
        .sum()
}

#[allow(clippy::too_many_arguments)]
fn paint_cell(
    buf: &mut Buffer,
    x: u16,
    row: u16,
    cell: &[InlineSpan],
    width: usize,
    align: CellAlign,
    default_fg: Color,
    bold: bool,
) -> u16 {
    let cell_w = span_text_width(cell);
    let trimmed = clip_spans(cell, width);
    let used = span_text_width(&trimmed).min(width);
    let pad_total = width.saturating_sub(used);
    let (left_pad, right_pad) = match align {
        CellAlign::Left => (0, pad_total),
        CellAlign::Right => (pad_total, 0),
        CellAlign::Center => (pad_total / 2, pad_total - pad_total / 2),
    };
    let mut col = x;
    if left_pad > 0 {
        col = paint_str(
            buf,
            col,
            row,
            &" ".repeat(left_pad),
            default_fg,
            BG,
            Modifier::empty(),
        );
    }
    for span in &trimmed {
        let (fg, mut mods) = style_to_paint(&span.style, default_fg);
        if bold {
            mods |= Modifier::BOLD;
        }
        col = paint_str(buf, col, row, &span.text, fg, BG, mods);
    }
    if right_pad > 0 {
        col = paint_str(
            buf,
            col,
            row,
            &" ".repeat(right_pad),
            default_fg,
            BG,
            Modifier::empty(),
        );
    }
    let _ = cell_w;
    col
}

fn clip_spans(spans: &[InlineSpan], width: usize) -> Vec<InlineSpan> {
    let mut out: Vec<InlineSpan> = Vec::new();
    let mut used = 0usize;
    for span in spans {
        let mut buf = String::new();
        for ch in span.text.chars() {
            let cw = UnicodeWidthChar::width(ch).unwrap_or(0);
            if used + cw > width {
                if width > 0 && used < width {
                    buf.push('…');
                }
                if !buf.is_empty() {
                    out.push(InlineSpan {
                        text: buf,
                        style: span.style.clone(),
                    });
                }
                return out;
            }
            buf.push(ch);
            used += cw;
        }
        if !buf.is_empty() {
            out.push(InlineSpan {
                text: buf,
                style: span.style.clone(),
            });
        }
    }
    out
}

fn block_rows(block: &MdBlock, width: u16) -> usize {
    let body_w = width;
    match block {
        MdBlock::Paragraph(spans) => wrap_spans(spans, body_w).len().max(1),
        MdBlock::Heading { .. } => 1,
        MdBlock::Code { text, .. } => text.lines().count().max(1),
        MdBlock::ListItem {
            ordered,
            index,
            depth,
            spans,
        } => {
            let indent = (*depth as usize * 2) as u16;
            let marker_w = indent
                + if *ordered {
                    let digits = ((*index as f64).log10().floor() as u16) + 1;
                    digits + 2
                } else {
                    2
                };
            wrap_spans(spans, body_w.saturating_sub(marker_w))
                .len()
                .max(1)
        }
        MdBlock::BlockQuote(spans) => wrap_spans(spans, body_w.saturating_sub(2)).len().max(1),
        MdBlock::Hr => 1,
        MdBlock::Table { head, rows, .. } => {
            let head_rows = if head.is_empty() { 0 } else { 2 };
            2 + head_rows + rows.len()
        }
    }
}

fn wrap_spans(spans: &[InlineSpan], width: u16) -> Vec<Vec<InlineSpan>> {
    let w = width.max(1) as usize;
    let mut out: Vec<Vec<InlineSpan>> = Vec::new();
    let mut current: Vec<InlineSpan> = Vec::new();
    let mut current_w = 0usize;
    let mut buf = String::new();
    for span in spans {
        buf.clear();
        for ch in span.text.chars() {
            let cw = UnicodeWidthChar::width(ch).unwrap_or(0);
            if current_w + cw > w && (current_w > 0 || !buf.is_empty()) {
                if !buf.is_empty() {
                    current.push(InlineSpan {
                        text: std::mem::take(&mut buf),
                        style: span.style.clone(),
                    });
                }
                out.push(std::mem::take(&mut current));
                current_w = 0;
            }
            buf.push(ch);
            current_w += cw;
        }
        if !buf.is_empty() {
            current.push(InlineSpan {
                text: std::mem::take(&mut buf),
                style: span.style.clone(),
            });
        }
    }
    if !current.is_empty() {
        out.push(current);
    }
    if out.is_empty() {
        out.push(Vec::new());
    }
    out
}

fn paint_styled(buf: &mut Buffer, x: u16, row: u16, spans: &[InlineSpan], default_fg: Color) {
    let mut col = x;
    for span in spans {
        let (fg, modifier) = style_to_paint(&span.style, default_fg);
        col = paint_str(buf, col, row, &span.text, fg, BG, modifier);
    }
}

fn style_to_paint(style: &InlineStyle, default_fg: Color) -> (Color, Modifier) {
    let mut mods = Modifier::empty();
    let mut fg = default_fg;
    if style.bold {
        mods |= Modifier::BOLD;
    }
    if style.italic {
        mods |= Modifier::ITALIC;
    }
    if style.strike {
        mods |= Modifier::CROSSED_OUT;
    }
    if style.code {
        fg = WARN;
    }
    if style.link {
        mods |= Modifier::UNDERLINED;
        fg = INFO;
    }
    (fg, mods)
}
