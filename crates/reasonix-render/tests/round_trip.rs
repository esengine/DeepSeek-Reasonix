use reasonix_render::scene::{
    BoxLayout, Color, FlexDirection, NamedColor, SceneFrame, SceneNode, TextRun, TextStyle,
};

#[test]
fn deserializes_a_simple_text_frame() {
    let json =
        r#"{"schemaVersion":1,"cols":80,"rows":24,"root":{"kind":"text","runs":[{"text":"hello"}]}}"#;
    let frame: SceneFrame = serde_json::from_str(json).expect("valid frame");
    assert_eq!(frame.schema_version, 1);
    assert_eq!(frame.cols, 80);
    assert_eq!(frame.rows, 24);
    match frame.root {
        SceneNode::Text { runs, wrap } => {
            assert_eq!(runs.len(), 1);
            assert_eq!(runs[0].text, "hello");
            assert!(runs[0].style.is_none());
            assert!(wrap.is_none());
        }
        SceneNode::Box { .. } => panic!("expected text root"),
    }
}

#[test]
fn deserializes_a_styled_run_with_named_color() {
    let json = r#"{"text":"ok","style":{"color":"green","bold":true}}"#;
    let run: TextRun = serde_json::from_str(json).expect("valid run");
    let style = run.style.expect("style present");
    assert_eq!(style.bold, Some(true));
    match style.color {
        Some(Color::Named(NamedColor::Green)) => {}
        other => panic!("expected named green, got {other:?}"),
    }
}

#[test]
fn deserializes_a_styled_run_with_hex_color() {
    let json = r##"{"text":"x","style":{"color":{"hex":"#aabbcc"}}}"##;
    let run: TextRun = serde_json::from_str(json).expect("valid run");
    let style = run.style.expect("style present");
    match style.color {
        Some(Color::Hex { hex }) => assert_eq!(hex, "#aabbcc"),
        other => panic!("expected hex color, got {other:?}"),
    }
}

#[test]
fn deserializes_a_box_with_layout_and_children() {
    let json = r#"{
        "schemaVersion": 1,
        "cols": 100,
        "rows": 30,
        "root": {
            "kind": "box",
            "layout": { "direction": "column", "paddingX": 1, "gap": 1 },
            "children": [
                { "kind": "text", "runs": [{ "text": "a" }] },
                { "kind": "text", "runs": [{ "text": "b" }] }
            ]
        }
    }"#;
    let frame: SceneFrame = serde_json::from_str(json).expect("valid frame");
    let SceneNode::Box { layout, children } = frame.root else {
        panic!("expected box root");
    };
    let layout = layout.expect("layout present");
    assert_eq!(layout.direction, Some(FlexDirection::Column));
    assert_eq!(layout.padding_x, Some(1));
    assert_eq!(layout.gap, Some(1));
    assert_eq!(children.len(), 2);
}

#[test]
fn round_trips_through_serde_without_field_loss() {
    let frame = SceneFrame {
        schema_version: 1,
        cols: 80,
        rows: 24,
        root: SceneNode::Box {
            layout: Some(BoxLayout {
                direction: Some(FlexDirection::Column),
                padding_x: Some(1),
                ..Default::default()
            }),
            children: vec![SceneNode::Text {
                runs: vec![TextRun {
                    text: "status: ok".to_string(),
                    style: Some(TextStyle {
                        bold: Some(true),
                        ..Default::default()
                    }),
                }],
                wrap: None,
            }],
        },
    };
    let json = serde_json::to_string(&frame).expect("serialize");
    let back: SceneFrame = serde_json::from_str(&json).expect("deserialize");
    assert_eq!(frame, back);
}

#[test]
fn rejects_unknown_node_kind() {
    let json =
        r#"{"schemaVersion":1,"cols":80,"rows":24,"root":{"kind":"image","src":"x.png"}}"#;
    let err = serde_json::from_str::<SceneFrame>(json).unwrap_err();
    let msg = err.to_string();
    assert!(msg.contains("kind") || msg.contains("variant"), "{msg}");
}
