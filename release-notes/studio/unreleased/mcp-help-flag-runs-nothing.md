## 修复

- 修复命令行里 `reasonix mcp import --help`（以及 `mcp remove --help` 等其他子命令）不会显示用法，反而把 `--help` 当成参数真的去执行：`import` 会直接改写 MCP 配置。现在所有 `reasonix mcp` 子命令的 `--help` / `-h` 都只打印用法并退出，什么都不改；`mcp import` 带多余参数时会报错而不是照常导入（#15022）。

---
<!-- lang:en -->

## Fixed

- `reasonix mcp import --help` (and `--help` on the other `mcp` subcommands) did not print usage and ran the command with `--help` as an argument, so `import` rewrote the MCP configuration. Every `reasonix mcp` subcommand now prints usage for `--help` / `-h` and changes nothing, and `mcp import` refuses extra arguments instead of importing (#15022).
