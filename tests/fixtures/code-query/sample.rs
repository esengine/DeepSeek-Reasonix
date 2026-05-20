pub struct User {
    pub name: String,
}

pub trait Greet {
    fn greet(&self) -> String;
}

pub fn hello(name: &str) -> String {
    format!("hi {}", name)
}

impl Greet for User {
    fn greet(&self) -> String {
        hello(&self.name)
    }
}

pub fn main() {
    let u = User { name: String::from("world") };
    println!("{}", u.greet());
}
