fn main() {
    let root_dir = std::path::Path::new(".");
    let php_dir = root_dir.join("php").join("src");
    let php_only_dir = root_dir.join("php_only").join("src");
    let common_dir = root_dir.join("common");

    let mut config = cc::Build::new();
    config.include(&php_dir);
    config
        .flag_if_supported("-std=c11")
        .flag_if_supported("-Wno-unused-parameter")
        .flag_if_supported("-Wno-unused-value")
        .flag_if_supported("-Wno-implicit-fallthrough");

    for path in &[
        php_dir.join("parser.c"),
        php_dir.join("scanner.c"),
        php_only_dir.join("parser.c"),
        php_only_dir.join("scanner.c"),
    ] {
        config.file(path);
        println!("cargo:rerun-if-changed={}", path.to_str().unwrap());
    }

    println!(
        "cargo:rerun-if-changed={}",
        common_dir.join("scanner.h").to_str().unwrap()
    );

    config.compile("tree-sitter-php");
}
