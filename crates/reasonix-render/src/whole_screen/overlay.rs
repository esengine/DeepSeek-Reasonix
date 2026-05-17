use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::Modifier;
use unicode_width::UnicodeWidthStr;

use crate::state::{SceneState, SlashMatch};

use super::paint::{paint, paint_str};
use super::theme::{BG, DS, DS_BRIGHT, FG, FG2, FG3};

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

/// True when `query` is `/<cmd>` for a `<cmd>` that exists in the catalog
/// (case-insensitive). The composer uses this to decide whether Enter
/// should auto-complete the highlighted match or submit the typed command
/// directly. Without this, typing the full name (e.g. `/cost`) re-completes
/// to `/cost ` and the user has to press Enter twice.
pub fn slash_is_exact(query: &str, state: &SceneState) -> bool {
    let Some(needle) = query.strip_prefix('/') else {
        return false;
    };
    if needle.is_empty() || needle.contains(' ') {
        return false;
    }
    let needle_lower = needle.to_lowercase();
    if let Some(catalog) = state.slash_catalog.as_ref() {
        return catalog
            .iter()
            .any(|m| m.cmd.eq_ignore_ascii_case(&needle_lower));
    }
    FALLBACK_COMMANDS.iter().any(|(name, _)| {
        name.trim_start_matches('/')
            .eq_ignore_ascii_case(&needle_lower)
    })
}

pub fn slash_completion(query: &str, idx: usize, state: &SceneState) -> Option<String> {
    if !query.starts_with('/') {
        return None;
    }
    match_iter(query, state)
        .nth(idx)
        .map(|name| format!("/{name} "))
}

/// Number of arg-completer matches for `/<cmd> <partial>`. Returns 0 when
/// the command has no static argCompleter, partial already exact-matches a
/// value, or partial has progressed past the first argument (contains a
/// space). Dynamic completers (`"models"`, `"path"`, `"mcp-resources"`,
/// `"mcp-prompts"`, `"skills"`) are not surfaced here yet — those live on
/// the Node side.
pub fn slash_arg_match_count(query: &str, state: &SceneState) -> usize {
    slash_arg_matches(query, state)
        .map(|v| v.len())
        .unwrap_or(0)
}

/// Build the full composer text after picking arg `idx` — replaces the
/// trailing partial with the chosen value. Returns None when there are
/// no matches.
pub fn slash_arg_completion(query: &str, idx: usize, state: &SceneState) -> Option<String> {
    let matches = slash_arg_matches(query, state)?;
    let chosen = matches.get(idx)?;
    let (cmd, _partial) = split_slash_arg(query)?;
    Some(format!("/{cmd} {chosen}"))
}

fn slash_arg_matches(query: &str, state: &SceneState) -> Option<Vec<String>> {
    let (cmd, partial) = split_slash_arg(query)?;
    if partial.contains(' ') {
        return None;
    }
    // Prefer the Node-resolved scene field: it's the only way to surface
    // dynamic completers (models / path / mcp-resources / mcp-prompts /
    // skills) because rust can't read MCP catalogs or the filesystem from
    // here. Match by cmd + partial so a stale push doesn't leak in.
    if let Some(s) = state.slash_arg_state.as_ref() {
        if s.cmd.eq_ignore_ascii_case(cmd) && s.partial == partial && !s.matches.is_empty() {
            return Some(s.matches.clone());
        }
    }
    let catalog = state.slash_catalog.as_ref()?;
    let m = catalog.iter().find(|m| m.cmd.eq_ignore_ascii_case(cmd))?;
    let completer = m.arg_completer.as_ref()?;
    let needle = partial.to_lowercase();
    if !partial.is_empty() && completer.iter().any(|v| v.to_lowercase() == needle) {
        return None;
    }
    if partial.is_empty() {
        return Some(completer.clone());
    }
    Some(
        completer
            .iter()
            .filter(|v| v.to_lowercase().starts_with(&needle))
            .cloned()
            .collect(),
    )
}

fn split_slash_arg(query: &str) -> Option<(&str, &str)> {
    let stripped = query.strip_prefix('/')?;
    let space_pos = stripped.find(' ')?;
    let cmd = &stripped[..space_pos];
    let partial = &stripped[space_pos + 1..];
    Some((cmd, partial))
}

fn match_iter<'a>(query: &'a str, state: &'a SceneState) -> Box<dyn Iterator<Item = &'a str> + 'a> {
    // strip_prefix removes ONE leading '/', not all of them — so "//" has
    // needle "/" which matches nothing (intended: user typed two slashes
    // as literal punctuation). trim_start_matches('/') would collapse
    // them and surface the whole catalog instead.
    let needle = query.strip_prefix('/').unwrap_or("").to_lowercase();
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

pub fn render_slash_arg_overlay(
    buf: &mut Buffer,
    dock_area: Rect,
    state: &SceneState,
    selected_idx: usize,
) {
    let Some(text) = state.composer_text.as_deref() else {
        return;
    };
    let Some(matches) = slash_arg_matches(text, state) else {
        return;
    };
    if matches.is_empty() {
        return;
    }
    let Some((cmd, partial)) = split_slash_arg(text) else {
        return;
    };
    let max_value_w = matches.iter().map(|s| s.width()).max().unwrap_or(8);
    let header = format!("/{cmd} <arg>");
    let header_w = header.width();
    let popup_w = (header_w.max(max_value_w + 4) as u16 + 4).min(dock_area.width.saturating_sub(4));
    if popup_w < 12 {
        return;
    }
    let cap = (dock_area.y as usize).saturating_sub(3).clamp(1, 10);
    let visible = matches.len().min(cap) as u16;
    let popup_h = 3 + visible;
    if popup_h > dock_area.y {
        return;
    }
    let popup_x = dock_area.x + 2;
    let popup_y = dock_area.y - popup_h;
    let popup = Rect::new(popup_x, popup_y, popup_w, popup_h);
    draw_box(buf, popup);
    paint_str(
        buf,
        popup.x + 2,
        popup.y + 1,
        &header,
        DS_BRIGHT,
        BG,
        Modifier::BOLD,
    );
    let count_text = if matches.len() == 1 {
        "1 option".to_string()
    } else {
        format!("{} options", matches.len())
    };
    let ccol = popup.x + popup.width.saturating_sub(count_text.width() as u16 + 2);
    paint_str(
        buf,
        ccol,
        popup.y + 1,
        &count_text,
        FG3,
        BG,
        Modifier::empty(),
    );
    let selected = selected_idx.min(matches.len().saturating_sub(1));
    let start = if selected >= visible as usize {
        selected + 1 - visible as usize
    } else {
        0
    };
    let needle = partial.to_lowercase();
    for (i, value) in matches
        .iter()
        .enumerate()
        .skip(start)
        .take(visible as usize)
    {
        let row = popup.y + 2 + (i - start) as u16;
        let is_sel = i == selected;
        if is_sel {
            paint_str(buf, popup.x + 2, row, "▸", DS_BRIGHT, BG, Modifier::BOLD);
        }
        let fg = if is_sel { FG } else { DS_BRIGHT };
        let modifier = if is_sel {
            Modifier::BOLD
        } else {
            Modifier::empty()
        };
        paint_str(buf, popup.x + 4, row, value, fg, BG, modifier);
        if !partial.is_empty() && value.to_lowercase().starts_with(&needle) {
            let suffix: String = value.chars().skip(partial.chars().count()).collect();
            let typed_w = partial.width() as u16;
            paint_str(
                buf,
                popup.x + 4 + typed_w,
                row,
                &suffix,
                FG2,
                BG,
                Modifier::empty(),
            );
        }
    }
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
        render_no_match(buf, dock_area, text);
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
    let header = if row.header_above { 1 } else { 0 };
    if !selected {
        return header + 1;
    }
    let desc = description_with_aliases(row);
    header + wrap_desc(&desc, layout.desc_w as usize).len().max(1)
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
    group: Option<String>,
    /// Render a group header row just above this command. Set true for
    /// the first row of each group when the user is browsing the bare
    /// `/` menu (group mode); always false in search mode.
    header_above: bool,
}

fn collect_rows(query: &str, state: &SceneState) -> Vec<SlashRow> {
    // See match_iter for why strip_prefix instead of trim_start_matches.
    let needle = query.strip_prefix('/').unwrap_or("").to_lowercase();
    let group_mode = needle.is_empty();
    let raw: Vec<SlashRow> = if let Some(catalog) = state.slash_catalog.as_ref() {
        catalog
            .iter()
            .filter(|m| matches_query(&m.cmd, &needle))
            .map(|m: &SlashMatch| SlashRow {
                name: format!("/{}", m.cmd),
                desc: m.summary.clone(),
                args_hint: m.args_hint.clone(),
                aliases: m.aliases.clone(),
                group: m.group.clone(),
                header_above: false,
            })
            .collect()
    } else {
        FALLBACK_COMMANDS
            .iter()
            .filter(|(name, _)| matches_query(name.trim_start_matches('/'), &needle))
            .map(|(name, desc)| SlashRow {
                name: (*name).to_string(),
                desc: (*desc).to_string(),
                args_hint: None,
                aliases: Vec::new(),
                group: None,
                header_above: false,
            })
            .collect()
    };
    if !group_mode {
        return raw;
    }
    let mut out = raw;
    let mut prev_group: Option<&str> = None;
    for row in out.iter_mut() {
        let g = row.group.as_deref();
        if g != prev_group {
            row.header_above = true;
            prev_group = g;
        }
    }
    out
}

fn render_no_match(buf: &mut Buffer, dock_area: Rect, query: &str) {
    let popup_w = dock_area.width.saturating_sub(4).min(80);
    if popup_w < 30 {
        return;
    }
    let popup_h: u16 = 3;
    if popup_h > dock_area.y {
        return;
    }
    let popup_x = dock_area.x + 2;
    let popup_y = dock_area.y - popup_h;
    let popup = Rect::new(popup_x, popup_y, popup_w, popup_h);
    draw_box(buf, popup);
    let msg = format!("no command matches '{query}'");
    paint_str(buf, popup.x + 2, popup.y + 1, "▲", FG2, BG, Modifier::BOLD);
    paint_str(
        buf,
        popup.x + 4,
        popup.y + 1,
        &msg,
        FG2,
        BG,
        Modifier::empty(),
    );
}

fn group_label(group: &str) -> &'static str {
    match group {
        "setup" => "SETUP",
        "info" => "INFO",
        "chat" => "CHAT",
        "extend" => "EXTEND",
        "session" => "SESSION",
        "code" => "CODE",
        "jobs" => "JOBS",
        "advanced" => "ADVANCED",
        _ => "OTHER",
    }
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
        if row_data.header_above {
            let label = row_data
                .group
                .as_deref()
                .map(group_label)
                .unwrap_or("OTHER");
            paint_str(buf, area.x + 2, row, label, FG3, BG, Modifier::BOLD);
            row += 1;
            if row >= bottom {
                break;
            }
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
