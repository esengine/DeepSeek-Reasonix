# MiMo 专项维护基线

> 基线：`main-v2` @ `15239191e`（v1.23.0）
> 范围：**纯 MiMo 局部配置** — 只影响 `mimo`（api.xiaomimimo.com）Responses provider，其他 vendor 与全局设置完全不受影响。

## 参数（`internal/provider/responses/vendor.go` `"mimo"` 段）

| 参数 | 值 | 必要性 |
|---|---|---|
| `singleSegmentReasoning` | `false` | MiMo 支持多段推理（tool-loop 中可起新 reasoning 段）。上游误标 `true` 会误导调用方按"单段"预期处理。 |
| `summaryMode` | `"none"` | **截断根因修复**：MiMo 服务端会把收到的 `reasoning.summary` 折回模型上下文，导致 thinking 逐轮翻倍膨胀直至截断。`"none"` 与 MiMo-Code SDK 的 `model_reasoning_summary = "none"` 对齐，明确告知服务端不要产出/回显 reasoning 摘要。 |
| `streamIdleTimeout` | `8 * time.Minute` | MiMo cold-path TTFT 可达 ~5 分钟（长 reasoning 轮次），默认 120s SSE 看门狗会误判失速提前中止流。 |
| `defaultMaxOutputTokens` | 32K（保持上游默认） | **不覆盖默认**：32K 覆盖 reasoning + 可见输出，长 reasoning 轮次会在可见正文前耗尽预算，截断工具调用 JSON。**声明**：默认 32K 不够用时，用户需在面板/配置手动调整到 `128000`（MiMo-Code 的 `MIMO_OUTPUT_TOKEN_MAX`，允许范围 [1, 131072]）。128K vs 32K 实测：成本 -40.9%、输入 token -67.6%、成功率 4/4 vs 3/4。 |
| `compactionOutputTokens` | `4096` | 压缩摘要用独立小预算（上游 16K-class 过宽），摘要调用不继承大输出默认。 |

配套逻辑（`internal/provider/responses/responses.go`）：

- `reasoning` 请求对象在 **effort 或 summaryMode 任一非空** 时发送（原逻辑仅 effort 非空）——保证 MiMo effort 默认 "auto"（归一化为空）时 `summaryMode="none"` 不被丢弃。
- SSE idle 超时改为 `cap.streamIdleTimeout`（零值回落 `defaultStreamIdleTimeout`）。
- `WarnOnMissingToolCallReasoning` 改为显式 `vendor == "mimo"` 特判（行为等价于上游经 `singleSegmentReasoning=true` 的不警告路径）。

## 为什么是纯 MiMo 局部（其他 vendor / 全局零影响）

1. **vendorTable 按 vendor 隔离**：`summaryMode`/`streamIdleTimeout` 仅 mimo 段设值；deepseek/dashscope/未知 vendor 全零值 = 默认行为。
2. **零值 fallback**：`idleTimeout: cap.streamIdleTimeout` 对非 mimo vendor 传入 0，`readStream` 中 `idle <= 0` 回落 `defaultStreamIdleTimeout`(120s)——与上游一致。
3. **reasoning 发送退化等价**：非 mimo vendor `summaryMode == ""`，新逻辑退化为上游逐字节相同的"仅 effort 非空才发"。
4. **warning 路径等价**：上游全表仅 mimo 为 `singleSegmentReasoning: true`；显式 mimo 特判与旧单段判断对 mimo 结果一致，其他 vendor 分支不变。
5. **不触碰全局设置**：`defaultMaxOutputTokens` 保持上游 32K 默认，128K 仅作为"用户手动调整"建议声明，无任何默认值/面板配置改动。

## 测试

- `TestMiMoEmitsSummaryModeEvenWhenEffortEmpty`：effort 为空时仍发送 `reasoning.summary="none"`。
- `TestVendorCapabilityTableCoversKnownEndpoints`：mimo `singleSegment` 列更新为 false。

## 维护方式

- 本基线建立在 `main-v2` 镜像之上，仅含上述 MiMo 专项改动。
- 上游 main-v2 更新后，重新基线：以新上游为 base 重放本文件的 mimo 专项 diff（`vendor.go`/`responses.go`/`responses_test.go`）。
- 供 PR 分支（如 `fix-mimo-reasoning-config` → 上游 #7644）及本地 dev 的 mimo 参数同步参照。
