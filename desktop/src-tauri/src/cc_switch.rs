use rusqlite::{Connection, OpenFlags};
use serde::Serialize;
use serde_json::Value;
use std::collections::HashMap;
use std::fs;
use std::path::{Path, PathBuf};

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ImportedMcpServer {
    pub name: String,
    pub transport: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub command: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub args: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub env: Option<HashMap<String, String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub cwd: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub url: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub headers: Option<HashMap<String, String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub disabled: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub request_timeout_ms: Option<u64>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ImportCcSwitchMcpResult {
    pub source: String,
    pub path: String,
    pub servers: Vec<ImportedMcpServer>,
}

#[tauri::command]
pub fn import_cc_switch_mcp() -> Result<ImportCcSwitchMcpResult, String> {
    let base_dir = cc_switch_dir();
    let db_path = base_dir.join("cc-switch.db");
    let cfg_path = base_dir.join("config.json");

    if db_path.exists() {
        match load_from_db(&db_path) {
            Ok(servers) if !servers.is_empty() => {
                return Ok(ImportCcSwitchMcpResult {
                    source: "db".to_string(),
                    path: db_path.display().to_string(),
                    servers,
                });
            }
            Ok(_) => {
                return Err(format!("No MCP servers found in {}", db_path.display()));
            }
            Err(db_err) => {
                if cfg_path.exists() {
                    let fallback = load_from_config(&cfg_path).map_err(|cfg_err| {
                        format!(
                            "Failed to read {} ({db_err}) and {} ({cfg_err})",
                            db_path.display(),
                            cfg_path.display()
                        )
                    })?;
                    if fallback.is_empty() {
                        return Err(format!(
                            "No MCP servers found in {} or {}",
                            db_path.display(),
                            cfg_path.display()
                        ));
                    }
                    return Ok(ImportCcSwitchMcpResult {
                        source: "config".to_string(),
                        path: cfg_path.display().to_string(),
                        servers: fallback,
                    });
                }
                return Err(format!("Failed to read {}: {db_err}", db_path.display()));
            }
        }
    }

    if cfg_path.exists() {
        let servers = load_from_config(&cfg_path)?;
        if servers.is_empty() {
            return Err(format!("No MCP servers found in {}", cfg_path.display()));
        }
        return Ok(ImportCcSwitchMcpResult {
            source: "config".to_string(),
            path: cfg_path.display().to_string(),
            servers,
        });
    }

    Err(format!(
        "cc-switch config not found. Expected {} or {}",
        db_path.display(),
        cfg_path.display()
    ))
}

fn cc_switch_dir() -> PathBuf {
    dirs::home_dir()
        .unwrap_or_else(|| PathBuf::from("."))
        .join(".cc-switch")
}

fn load_from_db(path: &Path) -> Result<Vec<ImportedMcpServer>, String> {
    let conn = Connection::open_with_flags(path, OpenFlags::SQLITE_OPEN_READ_ONLY)
        .map_err(|err| err.to_string())?;
    let columns = mcp_server_columns(&conn)?;
    let config_column = if columns.iter().any(|column| column == "server") {
        "server"
    } else if columns.iter().any(|column| column == "server_config") {
        "server_config"
    } else {
        return Err("mcp_servers table has no server or server_config column".to_string());
    };
    let has_name_column = columns.iter().any(|column| column == "name");
    let name_column = if has_name_column { "name" } else { "id" };
    let order_by = if has_name_column {
        "name ASC, id ASC"
    } else {
        "id ASC"
    };
    let query =
        format!("SELECT id, {name_column}, {config_column} FROM mcp_servers ORDER BY {order_by}");
    let mut stmt = conn.prepare(&query).map_err(|err| err.to_string())?;
    let rows = stmt
        .query_map([], |row| {
            let id: String = row.get(0)?;
            let name: String = row.get(1)?;
            let config: String = row.get(2)?;
            Ok((id, name, config))
        })
        .map_err(|err| err.to_string())?;

    let mut servers = Vec::new();
    for row in rows {
        let (id, name, config) = row.map_err(|err| err.to_string())?;
        let parsed: Value = serde_json::from_str(&config).map_err(|err| err.to_string())?;
        let display_name = name.trim();
        let import_name = if display_name.is_empty() {
            id.as_str()
        } else {
            display_name
        };
        if let Some(server) = parse_server(import_name, &parsed) {
            servers.push(server);
        }
    }
    Ok(servers)
}

fn mcp_server_columns(conn: &Connection) -> Result<Vec<String>, String> {
    let mut stmt = conn
        .prepare("PRAGMA table_info(mcp_servers)")
        .map_err(|err| err.to_string())?;
    let rows = stmt
        .query_map([], |row| row.get::<_, String>(1))
        .map_err(|err| err.to_string())?;
    let mut columns = Vec::new();
    for row in rows {
        columns.push(row.map_err(|err| err.to_string())?);
    }
    Ok(columns)
}

fn load_from_config(path: &Path) -> Result<Vec<ImportedMcpServer>, String> {
    let raw = fs::read_to_string(path).map_err(|err| err.to_string())?;
    let parsed: Value = serde_json::from_str(&raw).map_err(|err| err.to_string())?;
    let Some(mcp_root) = parsed.get("mcp") else {
        return Ok(Vec::new());
    };
    let Some(servers) = mcp_root.get("servers").and_then(Value::as_object) else {
        return Ok(Vec::new());
    };

    let mut out = Vec::new();
    for (id, entry) in servers {
        let Some(spec) = entry.get("server") else {
            continue;
        };
        if let Some(server) = parse_server(id, spec) {
            out.push(server);
        }
    }
    out.sort_by(|a, b| a.name.cmp(&b.name));
    Ok(out)
}

fn parse_server(id: &str, value: &Value) -> Option<ImportedMcpServer> {
    let name = id.trim();
    if name.is_empty() {
        return None;
    }
    let obj = value.as_object()?;
    let transport = detect_transport(obj);
    match transport.as_str() {
        "stdio" => {
            let command = obj.get("command")?.as_str()?.trim().to_string();
            if command.is_empty() {
                return None;
            }
            Some(ImportedMcpServer {
                name: name.to_string(),
                transport,
                command: Some(command),
                args: Some(read_string_array(obj.get("args"))),
                env: read_string_map(obj.get("env")),
                cwd: read_optional_string(obj.get("cwd")),
                url: None,
                headers: None,
                disabled: read_optional_bool(obj.get("disabled")),
                request_timeout_ms: read_request_timeout_ms(obj),
            })
        }
        "sse" | "streamable-http" => {
            let url = obj.get("url")?.as_str()?.trim().to_string();
            if url.is_empty() {
                return None;
            }
            Some(ImportedMcpServer {
                name: name.to_string(),
                transport,
                command: None,
                args: None,
                env: None,
                cwd: None,
                url: Some(url),
                headers: read_string_map(obj.get("headers")),
                disabled: read_optional_bool(obj.get("disabled")),
                request_timeout_ms: read_request_timeout_ms(obj),
            })
        }
        _ => None,
    }
}

fn detect_transport(obj: &serde_json::Map<String, Value>) -> String {
    let declared = obj
        .get("transport")
        .or_else(|| obj.get("type"))
        .and_then(Value::as_str)
        .unwrap_or("");
    match declared {
        "http" | "streamable-http" => "streamable-http".to_string(),
        "sse" => "sse".to_string(),
        "stdio" => "stdio".to_string(),
        _ => match obj.get("url").and_then(Value::as_str) {
            Some(url) if url.starts_with("http://") || url.starts_with("https://") => {
                "sse".to_string()
            }
            _ => "stdio".to_string(),
        },
    }
}

fn read_optional_string(value: Option<&Value>) -> Option<String> {
    value
        .and_then(Value::as_str)
        .map(str::trim)
        .filter(|entry| !entry.is_empty())
        .map(ToString::to_string)
}

fn read_optional_bool(value: Option<&Value>) -> Option<bool> {
    match value {
        Some(Value::Bool(flag)) => Some(*flag),
        Some(Value::String(text)) => match text.trim().to_ascii_lowercase().as_str() {
            "true" => Some(true),
            "false" => Some(false),
            _ => None,
        },
        _ => None,
    }
}

fn read_request_timeout_ms(obj: &serde_json::Map<String, Value>) -> Option<u64> {
    obj.get("requestTimeoutMs")
        .or_else(|| obj.get("timeoutMs"))
        .and_then(read_optional_u64)
}

fn read_optional_u64(value: &Value) -> Option<u64> {
    match value {
        Value::Number(num) => num.as_u64(),
        Value::String(text) => text.trim().parse::<u64>().ok(),
        _ => None,
    }
}

fn read_string_array(value: Option<&Value>) -> Vec<String> {
    let Some(items) = value.and_then(Value::as_array) else {
        return Vec::new();
    };
    items
        .iter()
        .filter_map(Value::as_str)
        .map(ToString::to_string)
        .collect()
}

fn read_string_map(value: Option<&Value>) -> Option<HashMap<String, String>> {
    let Some(entries) = value.and_then(Value::as_object) else {
        return None;
    };
    let mut out = HashMap::new();
    for (key, entry) in entries {
        let Some(text) = entry.as_str() else { continue };
        if !text.is_empty() {
            out.insert(key.clone(), text.to_string());
        }
    }
    if out.is_empty() {
        None
    } else {
        Some(out)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use rusqlite::params;
    use serde_json::json;
    use std::time::{SystemTime, UNIX_EPOCH};

    fn temp_db_path(label: &str) -> PathBuf {
        let suffix = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("system time should be after epoch")
            .as_nanos();
        std::env::temp_dir().join(format!(
            "reasonix-cc-switch-{label}-{}-{suffix}.db",
            std::process::id()
        ))
    }

    #[test]
    fn parse_stdio_server_preserves_disabled_and_timeout() {
        let value = json!({
            "command": "npx",
            "args": ["-y", "pkg"],
            "disabled": true,
            "requestTimeoutMs": 120000
        });

        let server = parse_server("memory", &value).expect("server should parse");

        assert_eq!(server.name, "memory");
        assert_eq!(server.transport, "stdio");
        assert_eq!(server.disabled, Some(true));
        assert_eq!(server.request_timeout_ms, Some(120000));
    }

    #[test]
    fn parse_http_server_accepts_timeout_string() {
        let value = json!({
            "transport": "streamable-http",
            "url": "https://example.test/mcp",
            "disabled": "false",
            "timeoutMs": "90000"
        });

        let server = parse_server("remote", &value).expect("server should parse");

        assert_eq!(server.transport, "streamable-http");
        assert_eq!(server.disabled, Some(false));
        assert_eq!(server.request_timeout_ms, Some(90000));
    }

    #[test]
    fn load_from_db_reads_current_server_column() {
        let path = temp_db_path("server-column");
        {
            let conn = Connection::open(&path).expect("db should open");
            conn.execute_batch(
                r#"
                CREATE TABLE mcp_servers (
                    id TEXT PRIMARY KEY,
                    name TEXT NOT NULL,
                    server TEXT NOT NULL
                );
                "#,
            )
            .expect("schema should be created");
            let server = json!({
                "transport": "streamable-http",
                "url": "https://example.test/mcp"
            })
            .to_string();
            conn.execute(
                "INSERT INTO mcp_servers (id, name, server) VALUES (?1, ?2, ?3)",
                params!["123e4567-e89b-12d3-a456-426614174000", "Remote", server],
            )
            .expect("row should insert");
        }

        let servers = load_from_db(&path).expect("db should load");
        fs::remove_file(&path).ok();

        assert_eq!(servers.len(), 1);
        assert_eq!(servers[0].name, "Remote");
        assert_eq!(servers[0].transport, "streamable-http");
        assert_eq!(servers[0].url.as_deref(), Some("https://example.test/mcp"));
    }

    #[test]
    fn load_from_db_falls_back_to_legacy_server_config_column() {
        let path = temp_db_path("server-config-column");
        {
            let conn = Connection::open(&path).expect("db should open");
            conn.execute_batch(
                r#"
                CREATE TABLE mcp_servers (
                    id TEXT PRIMARY KEY,
                    name TEXT NOT NULL,
                    server_config TEXT NOT NULL
                );
                "#,
            )
            .expect("schema should be created");
            let server = json!({
                "command": "npx",
                "args": ["-y", "pkg"]
            })
            .to_string();
            conn.execute(
                "INSERT INTO mcp_servers (id, name, server_config) VALUES (?1, ?2, ?3)",
                params!["stdio", "Stdio", server],
            )
            .expect("row should insert");
        }

        let servers = load_from_db(&path).expect("db should load");
        fs::remove_file(&path).ok();

        assert_eq!(servers.len(), 1);
        assert_eq!(servers[0].name, "Stdio");
        assert_eq!(servers[0].transport, "stdio");
        assert_eq!(servers[0].command.as_deref(), Some("npx"));
    }
}
