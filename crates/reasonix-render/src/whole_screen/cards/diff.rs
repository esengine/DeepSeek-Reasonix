use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::Modifier;

use crate::state::SceneCard;

use super::super::paint::{paint, paint_str};
use super::super::theme::{BG, DS_BRIGHT, ERR, FG1, FG2, FG3, OK};

pub(super) fn render_diff_card(
    buf: &mut Buffer,
    area: Rect,
    start_row: u16,
    card: &SceneCard,
) -> u16 {
    let mut row = render_diff_header(buf, area, start_row, card);
    if let Some(body) = card.body.as_deref() {
        row = render_diff_body(buf, area, row, body);
    }
    row.saturating_add(1)
}

fn render_diff_header(buf: &mut Buffer, area: Rect, row: u16, card: &SceneCard) -> u16 {
    let bottom = area.y + area.height;
    if row >= bottom {
        return row;
    }
    let mut col = area.x;
    col = paint_str(buf, col, row, "▎ ", DS_BRIGHT, BG, Modifier::empty());
    col = paint_str(buf, col, row, "± ", DS_BRIGHT, BG, Modifier::BOLD);
    col = paint_str(
        buf,
        col,
        row,
        &card.summary,
        DS_BRIGHT,
        BG,
        Modifier::empty(),
    );
    if let Some(meta) = card.meta.as_deref() {
        col = col.saturating_add(2);
        paint_meta(buf, col, row, meta);
    }
    row + 1
}

fn paint_meta(buf: &mut Buffer, x: u16, row: u16, meta: &str) {
    let mut col = x;
    for tok in meta.split_whitespace() {
        let (fg, modifier) = match tok.chars().next() {
            Some('+') => (OK, Modifier::BOLD),
            Some('-') | Some('−') => (ERR, Modifier::BOLD),
            _ => (FG2, Modifier::empty()),
        };
        col = paint_str(buf, col, row, tok, fg, BG, modifier);
        col = col.saturating_add(1);
    }
}

fn render_diff_body(buf: &mut Buffer, area: Rect, start_row: u16, body: &str) -> u16 {
    let bottom = area.y + area.height;
    let mut row = start_row;
    let mut old_ln: u32 = 0;
    let mut new_ln: u32 = 0;
    for line in body.split('\n') {
        if row >= bottom {
            break;
        }
        if let Some((o, n)) = parse_hunk_header(line) {
            old_ln = o;
            new_ln = n;
            render_hunk_line(buf, area, row, line);
            row += 1;
            continue;
        }
        let (kind, rest) = classify_line(line);
        render_diff_line(buf, area, row, kind, &mut old_ln, &mut new_ln, rest);
        row += 1;
    }
    row
}

fn render_hunk_line(buf: &mut Buffer, area: Rect, row: u16, line: &str) {
    let mut col = area.x;
    col = paint_str(buf, col, row, "▎", DS_BRIGHT, BG, Modifier::empty());
    col = col.saturating_add(1);
    paint_str(buf, col, row, line, FG2, BG, Modifier::ITALIC);
}

#[derive(Clone, Copy)]
enum DiffKind {
    Ctx,
    Add,
    Del,
}

fn classify_line(line: &str) -> (DiffKind, &str) {
    match line.chars().next() {
        Some('+') => (DiffKind::Add, &line[1..]),
        Some('-') => (DiffKind::Del, &line[1..]),
        _ => (DiffKind::Ctx, line.strip_prefix(' ').unwrap_or(line)),
    }
}

fn render_diff_line(
    buf: &mut Buffer,
    area: Rect,
    row: u16,
    kind: DiffKind,
    old_ln: &mut u32,
    new_ln: &mut u32,
    code: &str,
) {
    let mut col = area.x;
    col = paint_str(buf, col, row, "▎", DS_BRIGHT, BG, Modifier::empty());
    col = col.saturating_add(1);

    let (ol_show, nl_show, sign_ch, code_fg, sign_fg) = match kind {
        DiffKind::Ctx => (Some(*old_ln), Some(*new_ln), ' ', FG1, FG2),
        DiffKind::Add => (None, Some(*new_ln), '+', OK, OK),
        DiffKind::Del => (Some(*old_ln), None, '-', ERR, ERR),
    };

    col = paint_str(
        buf,
        col,
        row,
        &fmt_lineno(ol_show),
        FG3,
        BG,
        Modifier::empty(),
    );
    col = col.saturating_add(1);
    col = paint_str(
        buf,
        col,
        row,
        &fmt_lineno(nl_show),
        FG3,
        BG,
        Modifier::empty(),
    );
    col = col.saturating_add(1);
    paint(buf, col, row, sign_ch, sign_fg, BG, Modifier::empty());
    col = col.saturating_add(2);

    paint_str(buf, col, row, code, code_fg, BG, Modifier::empty());

    match kind {
        DiffKind::Ctx => {
            *old_ln += 1;
            *new_ln += 1;
        }
        DiffKind::Add => *new_ln += 1,
        DiffKind::Del => *old_ln += 1,
    }
}

fn fmt_lineno(n: Option<u32>) -> String {
    match n {
        Some(v) => format!("{v:>3}"),
        None => "   ".to_string(),
    }
}

fn parse_hunk_header(line: &str) -> Option<(u32, u32)> {
    let s = line.strip_prefix("@@ -")?;
    let (old_part, rest) = s.split_once(' ')?;
    let old_start = old_part.split_once(',').map(|(a, _)| a).unwrap_or(old_part);
    let new_part = rest.strip_prefix('+')?;
    let new_end = new_part
        .find(|c: char| !c.is_ascii_digit() && c != ',')
        .unwrap_or(new_part.len());
    let new_section = &new_part[..new_end];
    let new_start = new_section
        .split_once(',')
        .map(|(a, _)| a)
        .unwrap_or(new_section);
    Some((old_start.parse().ok()?, new_start.parse().ok()?))
}
