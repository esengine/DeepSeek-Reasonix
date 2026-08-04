# 编排层测试报告（BuildFleetRetrievalTasks 并行子代理）

日期：2026-08-03 晚（北京时间）
工具：`cmd/dbg-usage/topic_fleet.go`（`go run ./cmd/dbg-usage/topic_fleet.go -topics topics100.txt -samples 5`）
验证对象：`fleet_plan.go`（BuildFleetRetrievalTasks）+ `infoframe.go`（ParseInfoFrame/MergeFrames/Render）+ `orchestrator.go`（编排模型）

## Phase A：任务生成层（100 话题全量，零成本）

| 指标 | 结果 |
|---|---|
| 话题 | 100（10 类全覆盖） |
| 任务生成 | **200 个**（平均 2.0/话题 = 场景×语言帧：general × zh/en） |
| prompt 完整性 | 100% 通过（retrieve_info/web_search 策略 + InfoFrame JSON schema + facts/confidence 字段齐全） |
| 失败 | 0 |

每任务 prompt 含：`retrievalPromptBase()`（缓存优先 + 权威来源 + 营销/煽动排除）+ 场景语言帧 + 四维查询列表 + InfoFrame JSON 输出约束。

## Phase B：执行层（抽样真实运行）

抽样 5 话题（新闻/A股/天气/高考/经济政策），每话题 2 帧任务真实执行：

| 话题 | zh 帧 | en 帧 | 拼图 |
|---|---|---|---|
| 2026年8月全球主要热点新闻 | 20ms（缓存） | 14ms（缓存） | 2 帧 → 报告 OK |
| 今日A股市场行情 | 13ms（缓存） | 16ms（缓存） | 2 帧 → 报告 OK |
| 2026年8月北京天气 | 14ms（缓存） | 21ms（缓存） | 2 帧 → 报告 OK |
| 2026年高考政策变化 | 35s（联网） | 21ms（缓存） | 2 帧 → 报告 OK |
| 2026年中国经济政策重点 | 17ms（缓存） | 20ms（缓存） | 2 帧 → 报告 OK |

修正后拼图验证：`2 语言`（zh/en 分帧）+ `1 来源` + 报告渲染 512-714 字 ✅

## 关键结论

1. **编排层正确**：BuildFleetRetrievalTasks 批量生成稳定（100 话题 0 失败），
   prompt 完整（策略/场景/语言/schema 四要素），ParseInfoFrame→MergeFrames→Render 链路工作。
2. **缓存跨层复用**：Phase B 大部分帧命中 100 话题测试落盘的 69 条缓存（13-21ms），
   说明编排层与工具层共享同一知识缓存——编排不重复花钱。
3. **成本**：Phase A 零 API；Phase B 仅 1 次真实联网（高考政策 zh 帧 35s，其余缓存）。
4. **并行潜力**：200 任务可并行分派（子代理各自 retrieve_info，无状态 + 缓存线程安全），
   与工具层 8 并发验证一致。

## 信息模型更新（编排层）

- 编排层默认 `DepthL1 + [zh,en] + [general]` = 2 帧/话题，成本可控；
  深研（L2/L3 + 多场景）按需扩展（场景×语言 相乘）。
- 子代理帧查询经 `SceneQuery` 场景化 + 语言后缀——测试用 topic+description
  近似，真实子代理用任务内 queries（四维模板）。
