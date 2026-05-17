use crossterm::event::{KeyCode, KeyEvent, KeyEventKind, KeyEventState, KeyModifiers};

use reasonix_render::input::{is_quit, translate_key};

fn press(code: KeyCode, modifiers: KeyModifiers) -> KeyEvent {
    KeyEvent {
        code,
        modifiers,
        kind: KeyEventKind::Press,
        state: KeyEventState::NONE,
    }
}

#[test]
fn translates_a_plain_char() {
    let e = press(KeyCode::Char('a'), KeyModifiers::NONE);
    let t = translate_key(&e).expect("translate");
    assert_eq!(t.code, "Char");
    assert_eq!(t.char, Some('a'));
    assert!(t.modifiers.is_empty());
}

#[test]
fn translates_enter_with_no_char() {
    let e = press(KeyCode::Enter, KeyModifiers::NONE);
    let t = translate_key(&e).expect("translate");
    assert_eq!(t.code, "Enter");
    assert_eq!(t.char, None);
}

#[test]
fn translates_ctrl_a_with_a_ctrl_modifier() {
    let e = press(KeyCode::Char('a'), KeyModifiers::CONTROL);
    let t = translate_key(&e).expect("translate");
    assert_eq!(t.char, Some('a'));
    assert_eq!(t.modifiers, vec!["ctrl"]);
}

#[test]
fn collects_multiple_modifiers_in_canonical_order() {
    let e = press(
        KeyCode::Char('x'),
        KeyModifiers::CONTROL | KeyModifiers::ALT | KeyModifiers::SHIFT,
    );
    let t = translate_key(&e).expect("translate");
    assert_eq!(t.modifiers, vec!["ctrl", "alt", "shift"]);
}

#[test]
fn renders_function_keys_as_f1_through_f12() {
    let t = translate_key(&press(KeyCode::F(5), KeyModifiers::NONE)).expect("translate");
    assert_eq!(t.code, "F5");
}

#[test]
fn skips_release_events_so_only_presses_emit() {
    let e = KeyEvent {
        code: KeyCode::Char('a'),
        modifiers: KeyModifiers::NONE,
        kind: KeyEventKind::Release,
        state: KeyEventState::NONE,
    };
    assert!(translate_key(&e).is_none());
}

#[test]
fn serializes_a_plain_key_to_one_compact_json_line() {
    let t = translate_key(&press(KeyCode::Enter, KeyModifiers::NONE)).expect("translate");
    let json = serde_json::to_string(&t).expect("serialize");
    assert_eq!(json, r#"{"event":"key","code":"Enter"}"#);
}

#[test]
fn serializes_a_char_event_with_a_char_field() {
    let t = translate_key(&press(KeyCode::Char('z'), KeyModifiers::NONE)).expect("translate");
    let json = serde_json::to_string(&t).expect("serialize");
    assert_eq!(json, r#"{"event":"key","code":"Char","char":"z"}"#);
}

#[test]
fn serializes_modifiers_as_a_string_array() {
    let t = translate_key(&press(KeyCode::Char('c'), KeyModifiers::CONTROL)).expect("translate");
    let json = serde_json::to_string(&t).expect("serialize");
    assert_eq!(
        json,
        r#"{"event":"key","code":"Char","char":"c","modifiers":["ctrl"]}"#
    );
}

#[test]
fn is_quit_matches_only_ctrl_c() {
    assert!(is_quit(&press(KeyCode::Char('c'), KeyModifiers::CONTROL)));
    assert!(!is_quit(&press(KeyCode::Char('c'), KeyModifiers::NONE)));
    assert!(!is_quit(&press(KeyCode::Char('d'), KeyModifiers::CONTROL)));
    assert!(!is_quit(&press(KeyCode::Esc, KeyModifiers::NONE)));
}
