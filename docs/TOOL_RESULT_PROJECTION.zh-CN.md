# Active Tool Result Projection

Active Tool Result Projection 是默认关闭的缓存实验：

```toml
[agent]
tool_result_projection = true
```

也可以只对一次 headless 运行启用：`reasonix run --tool-result-projection ...`。

到达旧结果阈值后，Reasonix 先在本地归档完整结果，只改写受保护的 recent/active-turn tail 之前的陈旧结果。provider 可见投影是确定性的：工具名、原始字节数、短 SHA-256 身份以及有界首尾内容；不含 archive 路径和时间。system prompt、tool schema、工具顺序、tool-call 配对、assistant reasoning、受 `keep` 保护的错误以及活动尾部都保持不变。

默认值为 `false`，用于保留历史 snip/prune 基线。下一次 provider 请求实际使用投影历史时，Usage metrics 会报告 `tool_results_projected` 和 `projection_saved_chars`。可用 `e2ebench -mode harness-ab` 比较 `baseline` 与 `projection`，或比较 `delivery` 与 `delivery-projection`，同时观察任务成功率、cache hit、token 和成本。
