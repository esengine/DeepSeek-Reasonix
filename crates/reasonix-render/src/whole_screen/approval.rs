use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier};
use unicode_width::UnicodeWidthStr;

use crate::state::Approval;

use super::paint::{paint, paint_str};
use super::theme::{BG, DS_BRIGHT, DS_PURPLE, ERR, FG, FG1, FG2, FG3, OK, WARN};

const MAX_BODY_ROWS: u16 = 12;

pub fn render_approval(
    buf: &mut Buffer,
    screen: Rect,
    approval: &Approval,
    choice_idx: usize,
    dock_h: u16,
) {
    let popup_w = screen.width.saturating_sub(8).min(110);
    if popup_w < 50 {
        return;
    }
    let popup_x = screen.x + 2;
    let accent = accent_for(approval);
    let body = body_lines(
        approval,
        popup_w.saturating_sub(6) as usize,
        choice_idx,
        accent,
    );
    let actions = action_cells(approval);
    let body_h = body.len().min(MAX_BODY_ROWS as usize) as u16;
    let popup_h = 2 + body_h + 3;
    let bottom_margin = dock_h.max(5) + 1;
    if popup_h + bottom_margin > screen.height {
        return;
    }
    let popup_y = screen.bottom().saturating_sub(popup_h + bottom_margin);
    if popup_y < screen.y {
        return;
    }
    let popup = Rect::new(popup_x, popup_y, popup_w, popup_h);

    draw_box(buf, popup, accent);
    draw_title(buf, popup, approval, accent);
    let body_top = popup.y + 1;
    for (i, line) in body.iter().take(MAX_BODY_ROWS as usize).enumerate() {
        paint_str(
            buf,
            popup.x + 2,
            body_top + 1 + i as u16,
            line,
            FG,
            BG,
            Modifier::empty(),
        );
    }
    let footer_y = popup.y + popup.height - 2;
    draw_actions(buf, popup, footer_y, &actions);
}

fn accent_for(approval: &Approval) -> Color {
    match approval {
        Approval::Plan { .. } | Approval::Edit { .. } => DS_PURPLE,
        Approval::Shell { .. } => OK,
        Approval::Path { .. } | Approval::Choice { .. } => DS_BRIGHT,
        Approval::Checkpoint { .. } => OK,
    }
}

fn draw_title(buf: &mut Buffer, area: Rect, approval: &Approval, accent: Color) {
    let row = area.y;
    let (glyph, kind, subtitle, dismiss) = title_parts(approval);
    let mut col = area.x + 2;
    col = paint_str(buf, col, row, glyph, accent, BG, Modifier::BOLD);
    col = col.saturating_add(1);
    col = paint_str(buf, col, row, kind, accent, BG, Modifier::BOLD);
    if !subtitle.is_empty() {
        col = col.saturating_add(2);
        paint_str(buf, col, row, &subtitle, FG2, BG, Modifier::empty());
    }
    let hint = format!("esc {dismiss}");
    let hint_col = area.x + area.width.saturating_sub(hint.width() as u16 + 2);
    paint_str(buf, hint_col, row, &hint, FG3, BG, Modifier::empty());
}

fn title_parts(approval: &Approval) -> (&'static str, &'static str, String, &'static str) {
    match approval {
        Approval::Plan { body, steps } => {
            let n = if steps.is_empty() {
                body.lines().filter(|l| !l.trim().is_empty()).count()
            } else {
                steps.len()
            };
            (
                "◇",
                "REVIEW PLAN",
                format!("{n} step{}", if n == 1 { "" } else { "s" }),
                "skip",
            )
        }
        Approval::Shell { command, .. } => {
            let head = first_chunk(command, 40);
            ("⚡", "SHELL EXEC", head, "dismiss")
        }
        Approval::Path { path, intent, .. } => (
            "▸",
            "PATH ACCESS",
            format!("{intent} {}", first_chunk(path, 50)),
            "deny",
        ),
        Approval::Edit { path, .. } => ("▣", "EDIT PREVIEW", first_chunk(path, 50), "dismiss"),
        Approval::Choice { options, .. } => {
            let n = options.len();
            (
                "◆",
                "CHOOSE",
                format!("{n} option{}", if n == 1 { "" } else { "s" }),
                "skip",
            )
        }
        Approval::Checkpoint {
            completed, total, ..
        } => (
            "◉",
            "STEP CHECKPOINT",
            format!("{completed}/{total}"),
            "stop",
        ),
    }
}

fn wrap_inline(line: &str, max: usize) -> Vec<String> {
    if line.is_empty() {
        return vec![String::new()];
    }
    let w = max.max(10);
    let mut out = Vec::new();
    let mut current = String::new();
    let mut current_w = 0usize;
    for ch in line.chars() {
        let cw = unicode_width::UnicodeWidthChar::width(ch).unwrap_or(0);
        if current_w + cw > w && !current.is_empty() {
            out.push(std::mem::take(&mut current));
            current_w = 0;
        }
        current.push(ch);
        current_w += cw;
    }
    if !current.is_empty() {
        out.push(current);
    }
    if out.is_empty() {
        out.push(String::new());
    }
    out
}

fn first_chunk(s: &str, max: usize) -> String {
    let mut out = String::new();
    let mut w = 0usize;
    for ch in s.chars() {
        let cw = unicode_width::UnicodeWidthChar::width(ch).unwrap_or(0);
        if w + cw > max {
            out.push('…');
            break;
        }
        out.push(ch);
        w += cw;
    }
    out
}

fn body_lines(approval: &Approval, max_w: usize, choice_idx: usize, accent: Color) -> Vec<String> {
    let _ = accent;
    match approval {
        Approval::Plan { body, steps } => {
            let mut out = Vec::new();
            if !steps.is_empty() {
                for (i, step) in steps.iter().enumerate().take(MAX_BODY_ROWS as usize) {
                    let marker = match step.status.as_str() {
                        "done" => "✓",
                        "running" => "◆",
                        "failed" => "✕",
                        "blocked" => "⊘",
                        "skipped" => "·",
                        _ => "○",
                    };
                    let label = first_chunk(&step.title, max_w.saturating_sub(8));
                    out.push(format!("{marker} [{}] {label}", i + 1));
                }
            } else {
                for raw in body.lines() {
                    for line in wrap_inline(raw, max_w) {
                        out.push(line);
                        if out.len() >= MAX_BODY_ROWS as usize {
                            return out;
                        }
                    }
                }
            }
            if out.is_empty() {
                out.push("(plan body is empty)".to_string());
            }
            out
        }
        Approval::Shell {
            command,
            cwd,
            timeout_sec,
        } => {
            let mut out = Vec::new();
            out.push(format!(
                "$ {}",
                first_chunk(command, max_w.saturating_sub(2))
            ));
            let mut meta = Vec::new();
            if let Some(dir) = cwd.as_deref() {
                meta.push(format!("cwd {dir}"));
            }
            if let Some(s) = timeout_sec {
                meta.push(format!("timeout {s}s"));
            }
            if !meta.is_empty() {
                out.push(String::new());
                out.push(meta.join("   "));
            }
            out
        }
        Approval::Path {
            path,
            intent,
            tool_name,
        } => {
            let mut out = Vec::new();
            out.push(format!("intent  {intent}"));
            out.push(format!("tool    {tool_name}"));
            out.push(format!(
                "path    {}",
                first_chunk(path, max_w.saturating_sub(8))
            ));
            out
        }
        Approval::Edit {
            path,
            search,
            replace,
        } => {
            let mut out = Vec::new();
            let s_lines = search.lines().count();
            let r_lines = replace.lines().count();
            out.push(format!("{path}   -{s_lines}  +{r_lines}"));
            out.push(String::new());
            for line in search.lines().take(4) {
                out.push(format!("- {}", first_chunk(line, max_w.saturating_sub(2))));
            }
            for line in replace.lines().take(4) {
                out.push(format!("+ {}", first_chunk(line, max_w.saturating_sub(2))));
            }
            out
        }
        Approval::Checkpoint {
            title,
            completed,
            total,
        } => {
            let mut out = Vec::new();
            if let Some(t) = title.as_deref() {
                out.push(format!(
                    "just finished: {}",
                    first_chunk(t, max_w.saturating_sub(15))
                ));
            }
            out.push(String::new());
            out.push(format!("progress: {completed} of {total} step(s) done"));
            out.push(String::new());
            out.push("continue executing, type a refinement, or stop here?".to_string());
            out
        }
        Approval::Choice {
            question, options, ..
        } => {
            let mut out = Vec::new();
            out.push(first_chunk(question, max_w));
            out.push(String::new());
            let sel = choice_idx.min(options.len().saturating_sub(1));
            for (i, opt) in options.iter().enumerate().take(MAX_BODY_ROWS as usize - 2) {
                let marker = if i == sel { "●" } else { "○" };
                let letter = (b'a' + i as u8) as char;
                out.push(format!(
                    "{letter})  {marker}  {}",
                    first_chunk(&opt.title, max_w.saturating_sub(8))
                ));
                if let Some(summary) = opt.summary.as_deref() {
                    if !summary.is_empty() {
                        out.push(format!(
                            "        {}",
                            first_chunk(summary, max_w.saturating_sub(8))
                        ));
                    }
                }
            }
            out
        }
    }
}

fn action_cells(approval: &Approval) -> Vec<(&'static str, &'static str)> {
    match approval {
        Approval::Plan { .. } => vec![
            ("↵", "approve"),
            ("r", "refine"),
            ("v", "revise"),
            ("n", "cancel"),
        ],
        Approval::Shell { .. } => vec![
            ("↵", "run"),
            ("a", "always allow this command"),
            ("n", "cancel"),
        ],
        Approval::Path { .. } => vec![("↵", "allow once"), ("a", "always allow"), ("n", "deny")],
        Approval::Edit { .. } => vec![
            ("↵", "apply"),
            ("r", "reject"),
            ("t", "rest of turn"),
            ("y", "flip to auto"),
        ],
        Approval::Checkpoint { .. } => vec![("↵", "continue"), ("r", "revise"), ("s", "stop")],
        Approval::Choice { allow_custom, .. } => {
            let mut v = vec![("↑↓", "move"), ("↵", "pick"), ("1-9", "jump")];
            if *allow_custom {
                v.push(("c", "custom"));
            }
            v.push(("esc", "cancel"));
            v
        }
    }
}

fn draw_actions(buf: &mut Buffer, area: Rect, row: u16, actions: &[(&str, &str)]) {
    let sep_row = row.saturating_sub(1);
    for x in (area.x + 1)..(area.x + area.width - 1) {
        paint(buf, x, sep_row, '┄', FG3, BG, Modifier::empty());
    }
    let mut col = area.x + 2;
    for (i, (key, label)) in actions.iter().enumerate() {
        if i > 0 {
            col = col.saturating_add(3);
        }
        col = paint_str(buf, col, row, key, FG1, BG, Modifier::BOLD);
        col = col.saturating_add(1);
        col = paint_str(buf, col, row, label, FG2, BG, Modifier::empty());
    }
}

fn draw_box(buf: &mut Buffer, area: Rect, accent: Color) {
    let w = area.width;
    if w < 2 {
        return;
    }
    let top = area.y;
    let bot = area.y + area.height - 1;
    let right = area.x + w - 1;

    paint(buf, area.x, top, '╭', accent, BG, Modifier::empty());
    paint(buf, right, top, '╮', accent, BG, Modifier::empty());
    for x in 1..w - 1 {
        paint(buf, area.x + x, top, '─', accent, BG, Modifier::empty());
    }
    for y in (top + 1)..bot {
        paint(buf, area.x, y, '│', accent, BG, Modifier::empty());
        paint(buf, right, y, '│', accent, BG, Modifier::empty());
        for x in 1..w - 1 {
            paint(buf, area.x + x, y, ' ', FG, BG, Modifier::empty());
        }
    }
    paint(buf, area.x, bot, '╰', accent, BG, Modifier::empty());
    paint(buf, right, bot, '╯', accent, BG, Modifier::empty());
    for x in 1..w - 1 {
        paint(buf, area.x + x, bot, '─', accent, BG, Modifier::empty());
    }
    let _ = (ERR, WARN); // reserved for risk-tag highlights
}
