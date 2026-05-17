use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::Modifier;
use unicode_width::UnicodeWidthStr;

use crate::state::{AtPickerEntry, AtState, SceneState};

use super::paint::{paint, paint_str};
use super::theme::{BG, DS_PURPLE, FG, FG2, FG3, WARN};

const ABS_MAX_ROWS: usize = 24;

fn at_token(buffer: &str) -> Option<(usize, &str)> {
    let at_pos = buffer.rfind('@')?;
    let prefix = &buffer[..at_pos];
    let suffix = &buffer[at_pos + 1..];
    let valid_prefix = prefix.is_empty() || prefix.chars().last().is_some_and(char::is_whitespace);
    if !valid_prefix {
        return None;
    }
    if suffix.chars().any(char::is_whitespace) {
        return None;
    }
    Some((at_pos, suffix))
}

fn entries_for(state: &SceneState) -> Option<&[AtPickerEntry]> {
    match state.at_state.as_ref()? {
        AtState::Browse { entries, .. } | AtState::Search { entries, .. } => {
            Some(entries.as_slice())
        }
    }
}

pub fn at_match_count(buffer: &str, state: &SceneState) -> usize {
    if at_token(buffer).is_none() {
        return 0;
    }
    entries_for(state).map(|e| e.len()).unwrap_or(0)
}

pub fn at_completion(buffer: &str, idx: usize, state: &SceneState) -> Option<String> {
    let (at_pos, _) = at_token(buffer)?;
    let entries = entries_for(state)?;
    let entry = entries.get(idx)?;
    let prefix = &buffer[..at_pos];
    let suffix = if entry.is_dir {
        format!("{}/", entry.insert_path.trim_end_matches('/'))
    } else {
        format!("{} ", entry.insert_path)
    };
    Some(format!("{prefix}@{suffix}"))
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
    if at_token(text).is_none() {
        return;
    }
    let Some(at_state) = state.at_state.as_ref() else {
        return;
    };
    let entries = match at_state {
        AtState::Browse { entries, .. } | AtState::Search { entries, .. } => entries.as_slice(),
    };

    let total = entries.len();
    let available_rows = (dock_area.y as usize).saturating_sub(3);
    let cap = available_rows.clamp(1, ABS_MAX_ROWS);
    let popup_w = dock_area.width.saturating_sub(4).min(120);
    if popup_w < 30 {
        return;
    }
    let popup_x = dock_area.x + 2;
    if total == 0 {
        let popup_h = 3u16;
        if popup_h > dock_area.y {
            return;
        }
        let popup_y = dock_area.y.saturating_sub(popup_h);
        let popup = Rect::new(popup_x, popup_y, popup_w, popup_h);
        draw_box(buf, popup);
        draw_header(buf, popup, at_state, 0, 0, 0);
        let label = match at_state {
            AtState::Browse { loading: true, .. } => "loading…",
            AtState::Browse { .. } => "(empty directory)",
            AtState::Search {
                searching: true, ..
            } => "scanning…",
            AtState::Search { .. } => "(no matches)",
        };
        paint_str(
            buf,
            popup.x + 4,
            popup.y + 2,
            label,
            FG3,
            BG,
            Modifier::ITALIC,
        );
        return;
    }
    let selected = selected_idx.min(total - 1);
    let visible_n = total.min(cap).max(1);
    let mut window_start = selected.saturating_sub(visible_n.saturating_sub(1));
    if selected < window_start {
        window_start = selected;
    }
    if window_start + visible_n > total {
        window_start = total.saturating_sub(visible_n);
    }
    let visible = visible_n as u16;
    let popup_h = 3 + visible;
    if popup_h > dock_area.y {
        return;
    }
    let popup_y = dock_area.y.saturating_sub(popup_h);
    let popup = Rect::new(popup_x, popup_y, popup_w, popup_h);
    let window = &entries[window_start..window_start + visible_n];

    draw_box(buf, popup);
    draw_header(buf, popup, at_state, total, window_start, visible_n);
    draw_rows(buf, popup, window, at_state, selected - window_start);
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

fn draw_header(
    buf: &mut Buffer,
    area: Rect,
    at_state: &AtState,
    total: usize,
    window_start: usize,
    visible: usize,
) {
    let row = area.y + 1;
    let mut col = area.x + 2;
    col = paint_str(buf, col, row, "@ ", DS_PURPLE, BG, Modifier::BOLD);
    let lead = match at_state {
        AtState::Browse { base_dir, .. } => {
            if base_dir.is_empty() {
                "/".to_string()
            } else {
                format!("{base_dir}/")
            }
        }
        AtState::Search { filter, .. } => format!("search: {filter}"),
    };
    col = paint_str(buf, col, row, &lead, DS_PURPLE, BG, Modifier::BOLD);
    let position = if total <= visible {
        format!("  {total} entries")
    } else {
        let end = (window_start + visible).min(total);
        format!("  {}-{}/{}", window_start + 1, end, total)
    };
    col = paint_str(buf, col, row, &position, FG2, BG, Modifier::empty());
    let status = match at_state {
        AtState::Browse { loading: true, .. } => " · loading…",
        AtState::Search {
            searching: true, ..
        } => " · scanning…",
        _ => "",
    };
    if !status.is_empty() {
        col = paint_str(buf, col, row, status, FG3, BG, Modifier::empty());
    }
    let _ = col;
    let hint = "↑↓ move  ↵ select  ⇥ drill  esc dismiss";
    let hcol = area.x + area.width.saturating_sub(hint.width() as u16 + 2);
    paint_str(buf, hcol, row, hint, FG3, BG, Modifier::empty());
}

fn draw_rows(
    buf: &mut Buffer,
    area: Rect,
    entries: &[AtPickerEntry],
    at_state: &AtState,
    selected_idx: usize,
) {
    let body_top = area.y + 2;
    let max_w = area.width.saturating_sub(6);
    let filter = match at_state {
        AtState::Search { filter, .. } => filter.as_str(),
        AtState::Browse { .. } => "",
    };
    for (i, entry) in entries.iter().enumerate().take(ABS_MAX_ROWS) {
        let row = body_top + i as u16;
        let selected = i == selected_idx;
        if selected {
            paint_str(buf, area.x + 2, row, "▸", DS_PURPLE, BG, Modifier::BOLD);
        }
        let name_col = area.x + 4;
        let label = if entry.is_dir {
            format!("{}/", entry.label.trim_end_matches('/'))
        } else {
            entry.label.clone()
        };
        let suffix_label = entry.dir_suffix.as_str();
        paint_entry(
            buf,
            name_col,
            row,
            &label,
            suffix_label,
            filter,
            max_w,
            selected,
            entry.is_dir,
        );
    }
}

#[allow(clippy::too_many_arguments)]
fn paint_entry(
    buf: &mut Buffer,
    x: u16,
    row: u16,
    label: &str,
    suffix: &str,
    filter: &str,
    max_w: u16,
    selected: bool,
    is_dir: bool,
) {
    let base_fg = if selected { FG } else { FG2 };
    let base_mod = if selected || is_dir {
        Modifier::BOLD
    } else {
        Modifier::empty()
    };
    let mut budget = max_w;
    let mut col = x;
    if let Some((before, mid, after)) = case_insensitive_split(label, filter) {
        col = paint_clipped(buf, col, row, before, base_fg, base_mod, &mut budget);
        col = paint_clipped(buf, col, row, mid, WARN, Modifier::BOLD, &mut budget);
        col = paint_clipped(buf, col, row, after, base_fg, base_mod, &mut budget);
    } else {
        col = paint_clipped(buf, col, row, label, base_fg, base_mod, &mut budget);
    }
    if !suffix.is_empty() && budget > 1 {
        col = paint_clipped(buf, col, row, "  ", FG3, Modifier::empty(), &mut budget);
        let _ = paint_clipped(buf, col, row, suffix, FG3, Modifier::empty(), &mut budget);
    }
}

/// Char-boundary-safe case-insensitive substring split. Returns (before, mid, after) byte slices of `label` where `mid` is the matched range. Avoids byte indexing into `label.to_lowercase()` whose char-boundary mapping is not preserved across casefolding.
fn case_insensitive_split<'a>(label: &'a str, filter: &str) -> Option<(&'a str, &'a str, &'a str)> {
    if filter.is_empty() {
        return None;
    }
    let needle: Vec<char> = filter.chars().flat_map(char::to_lowercase).collect();
    if needle.is_empty() {
        return None;
    }
    let chars: Vec<(usize, char)> = label.char_indices().collect();
    let n = chars.len();
    'outer: for start in 0..n {
        let mut j = 0usize;
        let mut i = start;
        while j < needle.len() {
            if i >= n {
                continue 'outer;
            }
            let lc: Vec<char> = chars[i].1.to_lowercase().collect();
            if lc.len() != 1 || lc[0] != needle[j] {
                continue 'outer;
            }
            i += 1;
            j += 1;
        }
        let start_byte = chars[start].0;
        let end_byte = if i >= n { label.len() } else { chars[i].0 };
        return Some((
            &label[..start_byte],
            &label[start_byte..end_byte],
            &label[end_byte..],
        ));
    }
    None
}

fn paint_clipped(
    buf: &mut Buffer,
    x: u16,
    row: u16,
    s: &str,
    fg: ratatui::style::Color,
    modifier: Modifier,
    budget: &mut u16,
) -> u16 {
    if *budget == 0 {
        return x;
    }
    let mut w = 0u16;
    let mut clipped = String::new();
    for ch in s.chars() {
        let cw = unicode_width::UnicodeWidthChar::width(ch).unwrap_or(1) as u16;
        if w + cw > *budget {
            break;
        }
        w += cw;
        clipped.push(ch);
    }
    paint_str(buf, x, row, &clipped, fg, BG, modifier);
    *budget = budget.saturating_sub(w);
    x + w
}
