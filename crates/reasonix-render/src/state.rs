use serde::Deserialize;

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SceneState {
    #[serde(default)]
    pub model: Option<String>,
    #[serde(default)]
    pub card_count: u32,
    #[serde(default)]
    pub cards: Vec<SceneCard>,
    #[serde(default)]
    pub busy: bool,
    #[serde(default)]
    pub activity: Option<String>,
    #[serde(default)]
    pub composer_text: Option<String>,
    #[serde(default)]
    pub composer_cursor: Option<usize>,
    #[serde(default)]
    pub slash_matches: Option<Vec<SlashMatch>>,
    #[serde(default)]
    pub slash_selected_index: Option<usize>,
    #[serde(default)]
    pub approval_kind: Option<String>,
    #[serde(default)]
    pub approval_prompt: Option<String>,
    #[serde(default)]
    pub sessions: Option<Vec<SessionItem>>,
    #[serde(default)]
    pub sessions_focused_index: Option<usize>,
    #[serde(default)]
    pub wallet_balance: Option<f64>,
    #[serde(default)]
    pub wallet_currency: Option<String>,
    #[serde(default)]
    pub mcp_server_count: Option<u32>,
    #[serde(default)]
    pub edit_mode: Option<EditMode>,
    #[serde(default)]
    pub cwd: Option<String>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SceneCard {
    pub kind: String,
    #[serde(default)]
    pub summary: String,
    #[serde(default)]
    pub args: Option<String>,
    #[serde(default)]
    pub status: Option<ToolStatus>,
    #[serde(default)]
    pub elapsed: Option<String>,
    #[serde(default)]
    pub id: Option<String>,
    #[serde(default)]
    pub body: Option<String>,
    #[serde(default)]
    pub ts: Option<i64>,
    #[serde(default)]
    pub meta: Option<String>,
}

#[derive(Debug, Clone, Copy, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum ToolStatus {
    Ok,
    Err,
    Running,
}

#[derive(Debug, Clone, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum EditMode {
    Review,
    Auto,
    Yolo,
}

impl EditMode {
    pub fn as_str(&self) -> &'static str {
        match self {
            EditMode::Review => "review",
            EditMode::Auto => "auto",
            EditMode::Yolo => "yolo",
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct SlashMatch {
    pub cmd: String,
    #[serde(default)]
    pub summary: String,
    #[serde(default, rename = "argsHint")]
    pub args_hint: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct SessionItem {
    pub title: String,
    #[serde(default)]
    pub meta: Option<String>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SetupState {
    #[serde(default)]
    pub buffer_length: usize,
    #[serde(default)]
    pub error: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(tag = "type", rename_all = "camelCase")]
pub enum Message {
    Trace(SceneState),
    Setup(SetupState),
}

pub enum Payload {
    Trace(SceneState),
    Setup(SetupState),
}

pub fn decode_message(line: &str) -> Result<Payload, serde_json::Error> {
    if let Ok(msg) = serde_json::from_str::<Message>(line) {
        return Ok(match msg {
            Message::Trace(state) => Payload::Trace(state),
            Message::Setup(state) => Payload::Setup(state),
        });
    }
    if let Ok(state) = serde_json::from_str::<SceneState>(line) {
        return Ok(Payload::Trace(state));
    }
    let state: SetupState = serde_json::from_str(line)?;
    Ok(Payload::Setup(state))
}
