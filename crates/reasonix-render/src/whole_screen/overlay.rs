use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::Modifier;
use unicode_width::UnicodeWidthStr;

use crate::state::{SceneState, SlashMatch};

use super::paint::{paint, paint_str};
use super::theme::{BG, DS, DS_BRIGHT, FG, FG2};

const FALLBACK_COMMANDS: &[(&str, &str)] = &[
    ("/clear", "reset conversation context"),
    ("/compact", "summarize history to free up tokens"),
    ("/help", "show help"),
];

const ABS_MAX_ROWS: usize = 24;

pub fn slash_match_count(query: &str, state: &SceneState) -> usize {
    if !query.starts_with('/') {
        return 0;
    }
    match_iter(query, state).count()
}

pub fn slash_completion(query: &str, idx: usize, state: &SceneState) -> Option<String> {
    if !query.starts_with('/') {
        return None;
    }
    match_iter(query, state)
        .nth(idx)
        .map(|name| format!("/{name} "))
}

fn match_iter<'a>(query: &'a str, state: &'a SceneState) -> Box<dyn Iterator<Item = &'a str> + 'a> {
    let needle = query.trim_start_matches('/').to_lowercase();
    if let Some(catalog) = state.slash_catalog.as_ref() {
        return Box::new(
            catalog
                .iter()
                .filter(move |m| matches_query(&m.cmd, &needle))
                .map(|m| m.cmd.as_str()),
        );
    }
    Box::new(
        FALLBACK_COMMANDS
            .iter()
            .filter(move |(name, _)| matches_query(name.trim_start_matches('/'), &needle))
            .map(|(name, _)| name.trim_start_matches('/')),
    )
}

fn matches_query(cmd: &str, needle_lower: &str) -> bool {
    if needle_lower.is_empty() {
        return true;
    }
    cmd.to_lowercase().starts_with(needle_lower)
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
    let all_rows: Vec<SlashRow> = collect_rows(text, state);
    if all_rows.is_empty() {
        return;
    }
    let total = all_rows.len();
    let selected = selected_idx.min(total.saturating_sub(1));

    let popup_w = dock_area.width.saturating_sub(4).min(140);
    if popup_w < 40 {
        return;
    }
    let layout = compute_columns(&all_rows, popup_w);
    let heights: Vec<usize> = all_rows
        .iter()
        .enumerate()
        .map(|(i, r)| row_visual_height(r, &layout, i == selected))
        .collect();
    let available_rows = (dock_area.y as usize).saturating_sub(3);
    let max_visual_rows = available_rows.clamp(1, ABS_MAX_ROWS);

    let mut window_start = selected;
    let mut window_end = selected + 1;
    let mut used = heights[selected];
    while used < max_visual_rows {
        let grew = grow_window(
            &heights,
            &mut window_start,
            &mut window_end,
            &mut used,
            max_visual_rows,
            total,
        );
        if !grew {
            break;
        }
    }

    let visible_rows: u16 = heights[window_start..window_end]
        .iter()
        .sum::<usize>()
        .min(max_visual_rows) as u16;
    let popup_h = 3 + visible_rows;
    if popup_h > dock_area.y {
        return;
    }
    let popup_x = dock_area.x + 2;
    let popup_y = dock_area.y.saturating_sub(popup_h);
    let popup = Rect::new(popup_x, popup_y, popup_w, popup_h);

    draw_box(buf, popup);
    draw_header(buf, popup, total, window_start, window_end - window_start);
    draw_rows_wrapped(
        buf,
        popup,
        &all_rows[window_start..window_end],
        text,
        selected - window_start,
        &layout,
    );
}

fn grow_window(
    heights: &[usize],
    start: &mut usize,
    end: &mut usize,
    used: &mut usize,
    cap: usize,
    total: usize,
) -> bool {
    let next_below = if *end < total {
        Some(heights[*end])
    } else {
        None
    };
    let next_above = if *start > 0 {
        Some(heights[*start - 1])
    } else {
        None
    };
    if let Some(h) = next_below {
        if *used + h <= cap {
            *used += h;
            *end += 1;
            return true;
        }
    }
    if let Some(h) = next_above {
        if *used + h <= cap {
            *used += h;
            *start -= 1;
            return true;
        }
    }
    false
}

#[derive(Clone, Copy)]
struct ColumnLayout {
    name_col_off: u16,
    args_col_off: u16,
    desc_col_off: u16,
    desc_w: u16,
}

fn compute_columns(rows: &[SlashRow], popup_w: u16) -> ColumnLayout {
    const MAX_ARGS_HINT_W: u16 = 28;
    const MAX_NAME_W: u16 = 22;
    let raw_name_w =
        (rows.iter().map(|r| r.name.width()).max().unwrap_or(8) as u16).min(MAX_NAME_W);
    let raw_args_w = (rows
        .iter()
        .map(|r| r.args_hint.as_deref().map(|s| s.width()).unwrap_or(0))
        .max()
        .unwrap_or(0) as u16)
        .min(MAX_ARGS_HINT_W);
    let right_edge_off = popup_w.saturating_sub(2);
    let name_col_off = 4;
    let min_desc_w = 25u16;

    let want_args_col_off = name_col_off + raw_name_w + 2;
    let want_desc_col_off = if raw_args_w > 0 {
        want_args_col_off + raw_args_w + 2
    } else {
        want_args_col_off
    };

    if right_edge_off >= want_desc_col_off + min_desc_w {
        return ColumnLayout {
            name_col_off,
            args_col_off: want_args_col_off,
            desc_col_off: want_desc_col_off,
            desc_w: right_edge_off - want_desc_col_off,
        };
    }
    let gap_after_name = 2u16;
    let gap_after_args = if raw_args_w > 0 { 2 } else { 0 };
    let used_gaps = gap_after_name + gap_after_args;
    let cols_budget = right_edge_off
        .saturating_sub(name_col_off)
        .saturating_sub(min_desc_w)
        .saturating_sub(used_gaps);
    let total_natural = raw_name_w + raw_args_w;
    let max_name_w = if total_natural > 0 {
        ((u32::from(cols_budget) * u32::from(raw_name_w)) / u32::from(total_natural)) as u16
    } else {
        cols_budget
    };
    let max_args_w = cols_budget.saturating_sub(max_name_w);
    let args_col_off = name_col_off + max_name_w + gap_after_name;
    let desc_col_off = if max_args_w > 0 {
        args_col_off + max_args_w + gap_after_args
    } else {
        args_col_off
    };
    let desc_w = right_edge_off.saturating_sub(desc_col_off).max(10);
    ColumnLayout {
        name_col_off,
        args_col_off,
        desc_col_off,
        desc_w,
    }
}

fn description_with_aliases(row: &SlashRow) -> String {
    if row.aliases.is_empty() {
        row.desc.clone()
    } else {
        format!(
            "{}  · {}",
            row.desc,
            row.aliases
                .iter()
                .map(|a| format!("/{a}"))
                .collect::<Vec<_>>()
                .join(" ")
        )
    }
}

fn row_visual_height(row: &SlashRow, layout: &ColumnLayout, selected: bool) -> usize {
    if !selected {
        return 1;
    }
    let desc = description_with_aliases(row);
    wrap_desc(&desc, layout.desc_w as usize).len().max(1)
}

fn wrap_desc(text: &str, width: usize) -> Vec<String> {
    let w = width.max(1);
    if text.is_empty() {
        return vec![String::new()];
    }
    let mut out: Vec<String> = Vec::new();
    let mut current = String::new();
    let mut current_w = 0usize;
    for word in text.split_inclusive(' ') {
        let word_w: usize = word
            .chars()
            .map(|c| unicode_width::UnicodeWidthChar::width(c).unwrap_or(0))
            .sum();
        if current_w + word_w > w && !current.is_empty() {
            out.push(std::mem::take(&mut current));
            current_w = 0;
        }
        if word_w > w {
            for ch in word.chars() {
                let cw = unicode_width::UnicodeWidthChar::width(ch).unwrap_or(0);
                if current_w + cw > w && !current.is_empty() {
                    out.push(std::mem::take(&mut current));
                    current_w = 0;
                }
                current.push(ch);
                current_w += cw;
            }
        } else {
            current.push_str(word);
            current_w += word_w;
        }
    }
    if !current.is_empty() {
        out.push(current);
    }
    if out.is_empty() {
        out.push(String::new());
    }
    out
}

#[derive(Clone)]
struct SlashRow {
    name: String,
    desc: String,
    args_hint: Option<String>,
    aliases: Vec<String>,
}

fn collect_rows(query: &str, state: &SceneState) -> Vec<SlashRow> {
    let needle = query.trim_start_matches('/').to_lowercase();
    if let Some(catalog) = state.slash_catalog.as_ref() {
        return catalog
            .iter()
            .filter(|m| matches_query(&m.cmd, &needle))
            .map(|m: &SlashMatch| SlashRow {
                name: format!("/{}", m.cmd),
                desc: m.summary.clone(),
                args_hint: m.args_hint.clone(),
                aliases: m.aliases.clone(),
            })
            .collect();
    }
    FALLBACK_COMMANDS
        .iter()
        .filter(|(name, _)| matches_query(name.trim_start_matches('/'), &needle))
        .map(|(name, desc)| SlashRow {
            name: (*name).to_string(),
            desc: (*desc).to_string(),
            args_hint: None,
            aliases: Vec::new(),
        })
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

fn draw_header(buf: &mut Buffer, area: Rect, total: usize, window_start: usize, visible: usize) {
    let row = area.y + 1;
    let mut col = paint_str(
        buf,
        area.x + 2,
        row,
        "/ SLASH COMMANDS",
        DS_BRIGHT,
        BG,
        Modifier::BOLD,
    );
    let position = if total <= visible {
        format!("  {total} commands")
    } else {
        let end = (window_start + visible).min(total);
        format!("  {}-{}/{}", window_start + 1, end, total)
    };
    col = paint_str(buf, col, row, &position, FG2, BG, Modifier::empty());
    let _ = col;
    let hint = "↑↓ move  ↵ select  esc dismiss";
    let hcol = area.x + area.width.saturating_sub(hint.width() as u16 + 2);
    paint_str(buf, hcol, row, hint, FG2, BG, Modifier::empty());
}

fn draw_rows_wrapped(
    buf: &mut Buffer,
    area: Rect,
    rows_data: &[SlashRow],
    query: &str,
    selected_idx: usize,
    layout: &ColumnLayout,
) {
    let body_top = area.y + 2;
    let name_col = area.x + layout.name_col_off;
    let args_col = area.x + layout.args_col_off;
    let desc_col = area.x + layout.desc_col_off;
    let bottom = area.y + area.height - 1;
    let name_budget = layout.args_col_off.saturating_sub(layout.name_col_off + 1) as usize;
    let args_budget = layout.desc_col_off.saturating_sub(layout.args_col_off + 1) as usize;

    let mut row = body_top;
    for (i, row_data) in rows_data.iter().enumerate() {
        if row >= bottom {
            break;
        }
        let selected = i == selected_idx;

        let name_clipped = clip_with_ellipsis(&row_data.name, name_budget);
        if selected {
            paint_str(buf, area.x + 2, row, "▸", DS_BRIGHT, BG, Modifier::BOLD);
            paint_str(buf, name_col, row, &name_clipped, FG, BG, Modifier::BOLD);
        } else {
            paint_str(
                buf,
                name_col,
                row,
                &name_clipped,
                DS_BRIGHT,
                BG,
                Modifier::BOLD,
            );
        }

        if name_clipped.starts_with(query) && !name_clipped.ends_with('…') {
            let typed_w = query.width() as u16;
            let suffix: String = name_clipped.chars().skip(query.chars().count()).collect();
            paint_str(
                buf,
                name_col + typed_w,
                row,
                &suffix,
                FG2,
                BG,
                Modifier::empty(),
            );
        }

        if let Some(hint) = row_data.args_hint.as_deref() {
            let args_clipped = clip_with_ellipsis(hint, args_budget);
            paint_str(buf, args_col, row, &args_clipped, FG2, BG, Modifier::ITALIC);
        }

        let desc_fg = if selected { FG } else { FG2 };
        let desc = description_with_aliases(row_data);
        if selected {
            let lines = wrap_desc(&desc, layout.desc_w as usize);
            for (li, line) in lines.iter().enumerate() {
                let target = row + li as u16;
                if target >= bottom {
                    break;
                }
                paint_str(buf, desc_col, target, line, desc_fg, BG, Modifier::empty());
            }
            row += lines.len().max(1) as u16;
        } else {
            let clipped = clip_with_ellipsis(&desc, layout.desc_w as usize);
            paint_str(buf, desc_col, row, &clipped, desc_fg, BG, Modifier::empty());
            row += 1;
        }
    }
}

fn clip_with_ellipsis(text: &str, width: usize) -> String {
    let w = width.max(1);
    let total: usize = text
        .chars()
        .map(|c| unicode_width::UnicodeWidthChar::width(c).unwrap_or(0))
        .sum();
    if total <= w {
        return text.to_string();
    }
    let budget = w.saturating_sub(1);
    let mut out = String::new();
    let mut used = 0usize;
    for ch in text.chars() {
        let cw = unicode_width::UnicodeWidthChar::width(ch).unwrap_or(0);
        if used + cw > budget {
            break;
        }
        out.push(ch);
        used += cw;
    }
    out.push('…');
    out
}
