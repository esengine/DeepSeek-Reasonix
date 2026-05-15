use std::io::{self, BufRead, Write};
use std::process::ExitCode;

use reasonix_render::scene::SceneFrame;

fn main() -> ExitCode {
    let stdin = io::stdin();
    let stdout = io::stdout();
    let mut out = stdout.lock();
    let mut frame_count = 0u64;

    for (lineno, line) in stdin.lock().lines().enumerate() {
        let line = match line {
            Ok(l) => l,
            Err(e) => {
                eprintln!("read error on line {}: {}", lineno + 1, e);
                return ExitCode::from(2);
            }
        };
        if line.trim().is_empty() {
            continue;
        }
        let frame: SceneFrame = match serde_json::from_str(&line) {
            Ok(f) => f,
            Err(e) => {
                eprintln!("decode error on line {}: {}", lineno + 1, e);
                return ExitCode::from(2);
            }
        };
        frame_count += 1;
        writeln!(out, "frame {frame_count}: {frame:?}").ok();
    }
    ExitCode::SUCCESS
}
