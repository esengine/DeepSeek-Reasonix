use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct SceneFrame {
    #[serde(rename = "schemaVersion")]
    pub schema_version: u32,
    pub cols: u32,
    pub rows: u32,
    pub root: SceneNode,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "lowercase")]
pub enum SceneNode {
    Text {
        runs: Vec<TextRun>,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        wrap: Option<Wrap>,
    },
    Box {
        #[serde(default, skip_serializing_if = "Option::is_none")]
        layout: Option<BoxLayout>,
        children: Vec<SceneNode>,
    },
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct TextRun {
    pub text: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub style: Option<TextStyle>,
}

#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct TextStyle {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub color: Option<Color>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub bg: Option<Color>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub bold: Option<bool>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub dim: Option<bool>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub italic: Option<bool>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub underline: Option<bool>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub inverse: Option<bool>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub strikethrough: Option<bool>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(untagged)]
pub enum Color {
    Named(NamedColor),
    Hex { hex: String },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum NamedColor {
    Default,
    Black,
    Red,
    Green,
    Yellow,
    Blue,
    Magenta,
    Cyan,
    White,
    Gray,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum Wrap {
    Wrap,
    Truncate,
    TruncateStart,
    TruncateMiddle,
    None,
}

#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct BoxLayout {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub direction: Option<FlexDirection>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub gap: Option<i32>,
    #[serde(default, rename = "paddingX", skip_serializing_if = "Option::is_none")]
    pub padding_x: Option<i32>,
    #[serde(default, rename = "paddingY", skip_serializing_if = "Option::is_none")]
    pub padding_y: Option<i32>,
    #[serde(default, rename = "marginX", skip_serializing_if = "Option::is_none")]
    pub margin_x: Option<i32>,
    #[serde(default, rename = "marginY", skip_serializing_if = "Option::is_none")]
    pub margin_y: Option<i32>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub width: Option<Dim>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub height: Option<Dim>,
    #[serde(default, rename = "flexGrow", skip_serializing_if = "Option::is_none")]
    pub flex_grow: Option<i32>,
    #[serde(
        default,
        rename = "flexShrink",
        skip_serializing_if = "Option::is_none"
    )]
    pub flex_shrink: Option<i32>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub align: Option<FlexAlign>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub justify: Option<FlexJustify>,
    #[serde(
        default,
        rename = "borderStyle",
        skip_serializing_if = "Option::is_none"
    )]
    pub border_style: Option<BorderStyle>,
    #[serde(
        default,
        rename = "borderColor",
        skip_serializing_if = "Option::is_none"
    )]
    pub border_color: Option<Color>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub background: Option<Color>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum FlexDirection {
    Row,
    Column,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum FlexAlign {
    Start,
    Center,
    End,
    Stretch,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum FlexJustify {
    Start,
    Center,
    End,
    Between,
    Around,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum BorderStyle {
    Single,
    Double,
    Round,
    Bold,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(untagged)]
pub enum Dim {
    Cells(i32),
    Fill(FillToken),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum FillToken {
    Fill,
}
