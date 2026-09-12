# Reasonix 聊天内容与交互宿主移植

状态：已实施。本文原先的待实施方案已由当前实现和验收记录取代。完整中文说明见 [CHAT_CONTENT_HOST.zh-CN.md](CHAT_CONTENT_HOST.zh-CN.md)，英文说明见 [CHAT_CONTENT_HOST.md](CHAT_CONTENT_HOST.md)。

实现参考本地 DeepSeek Harness `c291e7961a515f6d7af9304e7fd1d257929aef26` 的聊天内容组织和交互方式。Reasonix 保留自己的会话协议、`present` 工具、权限体系、资源读取接口和 Electron 工作台，没有复制 Cordis 或 Harness 的整个 Conversation 框架。

关键默认决策已经落实：

- 所有聊天入口使用同一套自然文档流和 `ChatSource` 投影。
- 过程收起后卸载 Markdown、终端和工具详情等重内容，不保留 Harness 的隐藏 DOM。
- `present` 成功结果形成明确展示文件卡；成功的内置文件修改形成紧凑文件条目。
- 普通 Shell 文本、模型回答里的路径、失败调用和读取调用不能推导文件产物。
- 文件卡、工具路径和工作区文件使用统一资源引用和宿主操作入口。
- 工具详情使用“结果／参数／子调用／原始记录”四类按需标签。
- 本地和远程文件继续由宿主重新验证来源、会话、工作区和能力。
- provider 的 `present` schema、工具描述、工具顺序和提示词保持不变。

2026-09-13 的 Chromium、WebKit、Electron 原始数据和截图位于 [evidence/chat-content-host-2026-09-13](evidence/chat-content-host-2026-09-13)。
