fn greet(name: &str) -> String {
    format!("hi {name}")
}

fn main() {
    let _ = greet("world");
}
