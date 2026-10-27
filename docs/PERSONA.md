---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-18
---

# System persona / 系统人设

The user configuration may declare `[agent.persona]` with `prefix`, `suffix`
and `include_runtime_context`. Prefix and suffix supplement first-party guidance;
they cannot replace it. The retired `complete` field has no execution effect.
An absent persona preserves the existing default prompt.

用户配置可通过 `[agent.persona]` 设置 `prefix`、`suffix` 和
`include_runtime_context`。前后缀补充内置指令，不能替换它；旧 `complete`
字段不再影响执行。未配置人设时保持原有默认提示词。

Project `reasonix.toml` cannot set these fields. Loading restores the user's
values and reports an ignored user-only setting. This applies even when the
user has no persona configured.

项目 `reasonix.toml` 不能设置这些字段。加载时恢复用户配置，并报告忽略了项目中
的用户级设置；用户未配置人设时也同样如此。

`GET /persona` returns the user values and built-in starting points.
`POST /persona` saves `prefix`, `suffix` and optional `include_runtime_context`.
The text is trimmed and limited to 32 KiB per field, 64 KiB combined.

Saving does not rebuild a running session. New sessions and the next rebuild
of an existing session (for example, a model switch) read the saved persona.

`GET /persona` 返回用户设置和内置起始模板。`POST /persona` 保存 `prefix`、
`suffix` 及可选的 `include_runtime_context`。文本会去除首尾空白，每段最多
32 KiB，总计最多 64 KiB。保存本身不重建会话；新会话及现有会话下一次重建
（例如切换模型）时读取已保存的人设。

## Can different projects use different personas? / 不同项目能使用不同人设吗？

**The system persona is user-scoped, not project-scoped.** Use it for preferences
you want across projects. For project-specific roles, terminology and working
conventions, use standing instructions instead; this supplements the system
persona rather than configuring a separate `[agent.persona]`.

**系统人设属于用户级配置，不支持按项目分别配置。** 跨项目通用的偏好放在人设中；
项目特定的角色、术语和工作约定放在常驻指令中。这是对系统人设的补充，
不是为项目配置另一份 `[agent.persona]`。

### How do I add project guidance? / 如何添加项目指引？

Open the project as the session's workspace. Create or edit `REASONIX.md` at its
root, preserving any existing instructions. For example, one project's file
could contain:

将项目作为会话工作区打开，在项目根目录创建或编辑 `REASONIX.md`，保留已有指令。
例如，某个项目的文件可以包含：

```markdown
# Project guidance / 项目指引

- Act as a documentation reviewer for this project.
  在本项目中，以文档审阅者的视角工作。
- Use the terminology in docs/GLOSSARY.md when that file exists.
  若 docs/GLOSSARY.md 存在，使用其中的术语。
- Explain user-visible changes in both English and Chinese.
  用中英文说明用户可见的变更。
```

Give another project its own `REASONIX.md`. Commit shared conventions with the
project; use `REASONIX.local.md` in the same directory for personal instructions
(exclude it from version control yourself if it should stay private). Other
supported instruction files are loaded too, not replaced by `REASONIX.md`.

其他项目可使用各自的 `REASONIX.md`。团队约定随项目提交；个人指令可放在同目录的
`REASONIX.local.md` 中（如不希望提交，请自行将其排除在版本控制之外）。
其他受支持的指令文件也会加载，不会被 `REASONIX.md` 替换。

### When do edits apply, and what can they override? / 何时生效，能覆盖什么？

Standing instructions are rediscovered from disk for each turn. Save the file
before sending the next message; no `/reload` or new session is needed, and an
already-running turn is not retroactively changed. This differs from saving the
user persona, which takes effect only at the next session build described above.

常驻指令每轮都会从磁盘重新发现。发送下一条消息前保存文件即可，无须 `/reload`
或新建会话，也不会追溯修改正在执行的轮次。这与用户人设不同：保存人设后，
要等到上文所述的下一次会话构建才生效。

Project instructions do not replace built-in or higher-priority guidance, change
the user-only persona boundary, or grant tool permissions or sandbox access.
See the [Guide](./GUIDE.md) and [中文指南](./GUIDE.zh-CN.md) for hierarchical
standing instructions, rules, and permissions.

项目指令不能替换内置或更高优先级的指令，不能改变人设仅限用户级配置的边界，
也不能授予工具权限或沙箱访问权限。指令层级、规则和权限说明见
[Guide](./GUIDE.md) 和 [中文指南](./GUIDE.zh-CN.md)。
