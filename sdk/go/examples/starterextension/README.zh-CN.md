# Starter Extension

该目录是一个完整、可安装的 Extension Protocol v2 插件。它的 Sidecar
拦截 `input.receive`；以 `starter: ` 开头的文本会在送入模型前追加
` [rewritten by starter-extension]`。

二进制统一使用 `.exe` 后缀是有意设计：这样所有平台可以共用同一份
Manifest。Unix 可以正常执行该文件，Windows 则需要可执行文件后缀。

## 构建与安装

在 macOS 或 Linux 中进入本目录后运行：

```sh
go build -o bin/starter-extension.exe .
plugin_root="$(pwd -P)"
reasonix plugin install "$plugin_root" --dry-run
reasonix plugin install "$plugin_root" --link --replace --yes
```

Windows PowerShell：

```powershell
go build -o bin/starter-extension.exe .
$pluginRoot = (Resolve-Path .).Path
reasonix plugin install $pluginRoot --dry-run
reasonix plugin install $pluginRoot --link --replace --yes
```

安装前请检查 dry-run 输出中的 `FULL TRUST` 区块。链接安装会持续信任本目录
后续的内容变化，而且代码型 Runtime 运行在 Reasonix Sandbox 之外。

启动新会话，或在当前会话空闲时运行 `/reload`，然后发送：

```text
starter: 解释 Extension Protocol Sidecar 的作用
```

模型会收到改写后的文本。修改 `main.go`、重新构建、执行 `/reload` 后即可
再次验证。Manifest 或二进制校验失败时，运行
`reasonix plugin doctor starter-extension`。

## 下一步

- [`../../README.md`](../../README.md) 说明 SDK 回调与并发契约。
- [`../../../../docs/EXTENSIONS.zh-CN.md`](../../../../docs/EXTENSIONS.zh-CN.md)
  说明重载、性能、缓存、兼容性与信任模型。
- [`../../../../docs/PLUGIN_PACKAGES.zh-CN.md`](../../../../docs/PLUGIN_PACKAGES.zh-CN.md)
  定义 Manifest v2 的全部字段。
- [`../../../../docs/EXTENSION_PROTOCOL.zh-CN.md`](../../../../docs/EXTENSION_PROTOCOL.zh-CN.md)
  是线上协议参考。
- [`../fullsidecar/main.go`](../fullsidecar/main.go) 展示 Provider、结构化 UI、
  strategy、工具、content ref 与关闭流程。

要发布可分发插件，请为目标平台构建二进制，确保 Manifest runtime 路径与
包内文件一致，并提供不可变的源码或 release artifact 供用户安装前审查。
