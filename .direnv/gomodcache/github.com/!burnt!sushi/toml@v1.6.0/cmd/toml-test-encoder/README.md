# Implements the TOML test suite interface for TOML encoders

This is an implementation of the interface expected by
[toml-test](https://github.com/BurntSushi/toml-test) for the
[TOML encoder](https://github.com/BurntSushi/toml).
In particular, it maps JSON data on `stdin` to a TOML format on `stdout`.
