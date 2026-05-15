use crossterm::event::{KeyCode, KeyEvent, KeyEventKind, KeyModifiers};
use serde::Serialize;

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct InputEvent {
    pub event: &'static str,
    pub code: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub char: Option<char>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub modifiers: Vec<&'static str>,
}

pub fn translate_key(event: &KeyEvent) -> Option<InputEvent> {
    if event.kind != KeyEventKind::Press {
        return None;
    }
    let (code, ch) = match event.code {
        KeyCode::Char(c) => ("Char".to_string(), Some(c)),
        KeyCode::Enter => ("Enter".to_string(), None),
        KeyCode::Esc => ("Esc".to_string(), None),
        KeyCode::Up => ("Up".to_string(), None),
        KeyCode::Down => ("Down".to_string(), None),
        KeyCode::Left => ("Left".to_string(), None),
        KeyCode::Right => ("Right".to_string(), None),
        KeyCode::Backspace => ("Backspace".to_string(), None),
        KeyCode::Tab => ("Tab".to_string(), None),
        KeyCode::BackTab => ("BackTab".to_string(), None),
        KeyCode::Home => ("Home".to_string(), None),
        KeyCode::End => ("End".to_string(), None),
        KeyCode::PageUp => ("PageUp".to_string(), None),
        KeyCode::PageDown => ("PageDown".to_string(), None),
        KeyCode::Delete => ("Delete".to_string(), None),
        KeyCode::Insert => ("Insert".to_string(), None),
        KeyCode::F(n) => (format!("F{n}"), None),
        _ => return None,
    };
    Some(InputEvent {
        event: "key",
        code,
        char: ch,
        modifiers: collect_modifiers(event.modifiers),
    })
}

fn collect_modifiers(m: KeyModifiers) -> Vec<&'static str> {
    let mut out = Vec::new();
    if m.contains(KeyModifiers::CONTROL) {
        out.push("ctrl");
    }
    if m.contains(KeyModifiers::ALT) {
        out.push("alt");
    }
    if m.contains(KeyModifiers::SHIFT) {
        out.push("shift");
    }
    if m.contains(KeyModifiers::SUPER) {
        out.push("super");
    }
    out
}

pub fn is_quit(event: &KeyEvent) -> bool {
    event.kind == KeyEventKind::Press
        && event.code == KeyCode::Char('c')
        && event.modifiers.contains(KeyModifiers::CONTROL)
}
