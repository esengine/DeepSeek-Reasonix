use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color as RColor, Modifier};

use reasonix_render::render::render_frame;
use reasonix_render::scene::{
    BoxLayout, Color, FlexDirection, NamedColor, SceneFrame, SceneNode, TextRun, TextStyle,
};

fn frame_of(root: SceneNode) -> SceneFrame {
    SceneFrame {
        schema_version: 1,
        cols: 80,
        rows: 24,
        root,
    }
}

fn collect_row(buf: &Buffer, y: u16, width: u16) -> String {
    let mut out = String::new();
    for x in 0..width {
        out.push_str(buf[(x, y)].symbol());
    }
    out.trim_end().to_string()
}

#[test]
fn renders_a_plain_text_frame_at_row_zero() {
    let frame = frame_of(SceneNode::Text {
        runs: vec![TextRun {
            text: "hello".to_string(),
            style: None,
        }],
        wrap: None,
    });
    let area = Rect::new(0, 0, 10, 3);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(collect_row(&buf, 0, 10), "hello");
}

#[test]
fn applies_color_and_bold_to_text() {
    let frame = frame_of(SceneNode::Text {
        runs: vec![TextRun {
            text: "ok".to_string(),
            style: Some(TextStyle {
                color: Some(Color::Named(NamedColor::Green)),
                bold: Some(true),
                ..Default::default()
            }),
        }],
        wrap: None,
    });
    let area = Rect::new(0, 0, 5, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    let cell = &buf[(0, 0)];
    assert_eq!(cell.symbol(), "o");
    assert_eq!(cell.style().fg, Some(RColor::Green));
    assert!(cell.style().add_modifier.contains(Modifier::BOLD));
}

#[test]
fn stacks_column_children_vertically_with_gap() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            direction: Some(FlexDirection::Column),
            gap: Some(1),
            ..Default::default()
        }),
        children: vec![
            SceneNode::Text {
                runs: vec![TextRun {
                    text: "first".to_string(),
                    style: None,
                }],
                wrap: None,
            },
            SceneNode::Text {
                runs: vec![TextRun {
                    text: "second".to_string(),
                    style: None,
                }],
                wrap: None,
            },
        ],
    });
    let area = Rect::new(0, 0, 10, 5);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(collect_row(&buf, 0, 10), "first");
    assert_eq!(collect_row(&buf, 1, 10), "");
    assert_eq!(collect_row(&buf, 2, 10), "second");
}

#[test]
fn lays_out_row_children_horizontally() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            direction: Some(FlexDirection::Row),
            ..Default::default()
        }),
        children: vec![
            SceneNode::Text {
                runs: vec![TextRun {
                    text: "ab".to_string(),
                    style: None,
                }],
                wrap: None,
            },
            SceneNode::Text {
                runs: vec![TextRun {
                    text: "cd".to_string(),
                    style: None,
                }],
                wrap: None,
            },
        ],
    });
    let area = Rect::new(0, 0, 10, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(collect_row(&buf, 0, 10), "abcd");
}

#[test]
fn padding_shifts_children_in_and_shrinks_area() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            padding_x: Some(2),
            padding_y: Some(1),
            ..Default::default()
        }),
        children: vec![SceneNode::Text {
            runs: vec![TextRun {
                text: "x".to_string(),
                style: None,
            }],
            wrap: None,
        }],
    });
    let area = Rect::new(0, 0, 10, 5);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(2, 1)].symbol(), "x");
    assert_eq!(buf[(0, 0)].symbol(), " ");
}

#[test]
fn truncates_text_overflowing_its_area() {
    let frame = frame_of(SceneNode::Text {
        runs: vec![TextRun {
            text: "abcdefghij".to_string(),
            style: None,
        }],
        wrap: None,
    });
    let area = Rect::new(0, 0, 4, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(collect_row(&buf, 0, 4), "abcd");
}

#[test]
fn renders_hex_color_as_truecolor() {
    let frame = frame_of(SceneNode::Text {
        runs: vec![TextRun {
            text: "z".to_string(),
            style: Some(TextStyle {
                color: Some(Color::Hex {
                    hex: "#aabbcc".to_string(),
                }),
                ..Default::default()
            }),
        }],
        wrap: None,
    });
    let area = Rect::new(0, 0, 3, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(0, 0)].style().fg, Some(RColor::Rgb(0xaa, 0xbb, 0xcc)));
}
