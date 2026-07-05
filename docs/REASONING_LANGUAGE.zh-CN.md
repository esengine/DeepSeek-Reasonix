# 思考语言

<a href="./GUIDE.zh-CN.md">使用指南</a>
&nbsp;·&nbsp;
<a href="./REASONING_LANGUAGE.md">English</a>

`agent.reasoning_language` 控制模型服务暴露“可见思考过程”时，Reasonix 希望它优先使用哪种语言。

它不设置最终回答语言，不翻译代码、标识符或文件路径，也不改变模型内部不可见推理。用户在单次提问里明确要求的最终回答语言，仍然优先。

## 为什么需要它

有些用户希望可见思考过程更稳定地使用中文或英文，即使任务本身混合了多种语言。这个设置把这种偏好显式化，同时不改稳定 system prompt，也不改工具 schema。

取值只有三种：

- `auto`：用户原始提问明显为中文时锚定中文，并忽略 `@file` 等注入的引用内容；英文或不确定时不额外注入语言指令。
- `zh`：可见思考过程优先使用简体中文。
- `en`：可见思考过程优先使用英文。

## 桌面端

入口：

```text
设置 -> 模型 -> 使用 -> Agent 运行 -> 思考语言
```

桌面端设置会写入用户级默认值。项目仍然可以通过 `./reasonix.toml` 覆盖。

## CLI 与 TUI

在 shell 或脚本里修改：

```bash
reasonix config reasoning-language auto
reasonix config reasoning-language zh
reasonix config reasoning-language en
```

默认写入用户配置。要写入当前项目的覆盖配置：

```bash
reasonix config reasoning-language --local zh
```

在 `reasonix` 内可以用斜杠命令：

```text
/reasoning-language auto
/reasoning-language zh
/reasoning-language en
```

斜杠命令会写入用户级设置，并立即更新当前 chat controller，后续 turn 生效。它不会改写当前项目的 `reasonix.toml`；如果要写项目级覆盖，请使用带 `--local` 的 shell 命令。

单次 headless 运行也会读取同一设置：

```bash
reasonix run "解释这个模块"
```

## 配置文件

用户级或项目级配置：

```toml
[agent]
reasoning_language = "auto" # auto|zh|en
```

这个设置的配置优先级是：

```text
./reasonix.toml > 用户 config.toml > 内置默认值
```

目前没有为它提供命令行 flag。它更像用户偏好或项目偏好，不适合作为每次运行都传一次的任务参数。

## 缓存影响

`auto` 仍然对缓存友好。用户原始提问明显是中文时，Reasonix 会为这一轮加入同样很小的 `<reasoning-language>` 临时 block；英文或信号不明确时不注入，只复用已有的稳定语言策略。`@file` 等注入的引用内容不会参与这个自动判断。

当设置为 `zh` 或 `en` 时，Reasonix 总是会把一个很小的 `<reasoning-language>` 临时 block 放进本次 user turn。所有模式下，它都不会改变：

- system prompt
- 工具 schema 的字节或顺序
- provider 可见的稳定前缀

因此它能在表达明确偏好的同时，尽量保持高 prompt-cache 命中率。

## 边界

- 只有模型服务暴露可见思考文本时，这个设置才有意义。
- 它是偏好，不是强制翻译层。
- 代码、标识符、文件路径、shell 命令和未翻译技术术语应保留原样。
- 如果用户在某次提问中明确要求最终回答使用某种语言，最终回答仍以用户要求为准。
