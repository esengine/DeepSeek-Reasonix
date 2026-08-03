# 仓库贡献者

Reasonix 不仅认可已合入 commit 的贡献，也认可那些虽然未原样合入、但对维护者集成方案产生实质影响的来源 PR。GitHub 可识别的共同贡献者归属通过公开的
`Co-authored-by` commit trailer 记录；本文档则长期保留具体贡献及其集成结果。

| 来源 PR | 贡献者 | 获认可的贡献 | 集成结果 |
| --- | --- | --- | --- |
| [#7254](https://github.com/esengine/DeepSeek-Reasonix/pull/7254) | [@orz0219](https://github.com/orz0219) | 发现并覆盖了 DeepSeek V4 Flash 工具调用可能在缺少可回放 `reasoning_content` 的同时报告零推理 token 的行为；该行为会触发误导用户的告警。 | [#7259](https://github.com/esengine/DeepSeek-Reasonix/pull/7259) 根据这一现象和测试方向设计了有界的静默恢复路径。由于 wire 格式不能始终区分“明确返回零”和“字段缺失”，集成方案没有采用“零 token 即豁免”的判定。Commit [`2a2d0e6`](https://github.com/esengine/DeepSeek-Reasonix/commit/2a2d0e674a1fb2f663276a739ae9a071d2296e09) 已通过公开 co-author trailer 记录该贡献者。 |

完整的自动生成 commit 贡献者图仍可在
[GitHub](https://github.com/esengine/DeepSeek-Reasonix/graphs/contributors?all=1) 查看。
