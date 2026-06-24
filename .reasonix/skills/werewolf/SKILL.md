---
name: werewolf
description: 狼人杀 AI 主持人 — 管理 6-12 名 AI 玩家的昼夜循环
scripts:
  - scripts/werewolf_game.py
---

# 你是 GM，不是维护者

**你的唯一任务是推进游戏。不要探索代码库、不要审查引擎、不要改造。**

## 信任引擎

- 引擎输出什么就是什么。**不要验证、不要重算、不要怀疑。**
- `status-pretty --roles` 列的角色就是最终答案。你不需要再次核对人数或类型。
- 配置以 `config --show` 为准，不要自己去读 `werewolf_config.json`。
- 遇到不确定 → **执行下一步**，不要停下来分析。

## 常用命令

| 做什么 | 命令 |
|--------|------|
| 生成 prompt | `make-prompts <action> --with-history` |
| 执行夜间 | `night-auto --wolf ... --guard ... --seer ... --witch ...` |
| 执行白天 | `day-auto --speech "玩家:原文" --vote "玩家:原文"` |
| 警长竞选 | `sheriff --candidates ... --vote ...` |
| 警长方向 | `sheriff-direction 左\|右`（白天必须先做这步） |
| 保存 AI 原始回复 | `log-raw <type> <玩家> "<原文>"` |
| 看局面 | `summary [--with-history]` |

## 首夜执行卡（直接复制，勾选完成）

```
☐ 1. make-prompts night_guard --with-history → task 问守卫
☐ 2. make-prompts wolf_strategy --with-history → parallel_tasks 问 4 狼
☐ 3. task 悍跳狼汇总 → （如有分歧）make-prompts wolf_adjust
☐ 4. make-prompts witch --with-history → task 问女巫
☐ 5. make-prompts night_check --with-history → task 问预言家
☐ 6. night-auto --wolf "狼:刀X" --guard "守Y" --seer "查Z" --witch "救/毒X"
☐ 7. summary → 确认结果
```

## 第 1 天执行卡

```
☐ 1. make-prompts sheriff --player-count 12 → parallel_tasks 问上警意愿
☐ 2. task 候选人发言 → task 退水 → parallel_tasks 警下投票
☐ 3. sheriff --candidates ... --vote ...
☐ 4. sheriff-direction 左|右（警长当选后）
☐ 5. make-prompts speech --with-history → parallel_tasks 发言
☐ 6. make-prompts vote --with-history → parallel_tasks 投票
☐ 7. day-auto --speech "玩家:原文" --vote "玩家:原文"
☐ 8. summary → 确认处决结果
```

## 命令速查

### 单人询问（加 --for-player，只生成1份prompt）
| 场景 | 命令 |
|------|------|
| 守卫守谁 | make-prompts night_guard --for-player **守卫名** --with-history |
| 女巫救/毒 | make-prompts witch --for-player **女巫名** --with-history --kill **被刀者** |
| 预言家验谁 | make-prompts night_check --for-player **预言家名** --with-history |
| 猎人开枪 | make-prompts hunter_active --for-player **猎人名** --with-history |
| 白痴翻牌 | make-prompts idiot_reveal --for-player **白痴名** --with-history |
| 遗言 | make-prompts last_words --for-player **死者名** --with-history |

### 多人询问（不加 --for-player，全体生成）
| 场景 | 命令 |
|------|------|
| 狼队首夜讨论 | make-prompts wolf_strategy --with-history |
| 狼队后续夜讨论 | make-prompts wolf_adjust --with-history |
| 白天发言 | make-prompts speech --with-history |
| 白天投票 | make-prompts vote --with-history |
| 警长竞选意愿 | make-prompts sheriff --with-history --player-count 12 |

### 每轮操作前先写 todo

```
todo_write 任务1(status=in_progress) 任务2(pending) 任务3(pending) ...
```

只把当前要做的一项设为 `in_progress`，其余 `pending`。

### 做完一项后 complete_step

`complete_step` 的 `step` 字段**必须和 todo_write 中的文本逐字相同**：

```json
// 正确 ✅ — 逐字复制 todo 文本
complete_step "守卫行动 — 问李强守谁"

// 错误 ❌ — 拼接了结果，不匹配
complete_step "守卫行动 — 李强空过"
```

**技巧**：写 todo 后，complete 时直接复制粘贴 todo 里的文本，不要自己打。

### 主机自动推进

你 sign off 一步后主机自动把下一步标记为 `in_progress`，**不需要再写一次 todo_write**。所有步骤完成后用 `todo_write` 更新最终状态。

## 执行模板（直接复制）

### 模板 1：首夜（第 0 晚）

```
todo_write [
  {content:"守卫行动 — 问守卫", status:in_progress}
  {content:"狼队首夜讨论（提案→汇总→分工）", status:pending}
  {content:"女巫行动（救/毒）", status:pending}
  {content:"预言家查验", status:pending}
  {content:"执行夜间+死亡判定", status:pending}
]

→ make-prompts night_guard --with-history
→ task 问守卫
→ complete_step "守卫行动 — 问守卫"

→ make-prompts wolf_strategy --with-history
→ parallel_tasks 四狼提案
→ task 悍跳狼汇总拍板
→ task 确认分工
→ complete_step "狼队首夜讨论（提案→汇总→分工）"

→ make-prompts witch --with-history
→ task 问女巫
→ complete_step "女巫行动（救/毒）"

→ make-prompts night_check --with-history
→ task 问预言家
→ complete_step "预言家查验"

→ night-auto --wolf...
→ complete_step "执行夜间+死亡判定"
```

### 模板 2：第 1 天警长竞选

```
todo_write [
  {content:"警长竞选（上警→发言→退水→投票）", status:in_progress}
  {content:"白天发言+投票+处决", status:pending}
]

→ parallel_tasks 问上警意愿
→ task 候选人发言
→ task 问退水
→ parallel_tasks 警下投票
→ sheriff --candidates ... --vote ...
→ complete_step "警长竞选（上警→发言→退水→投票）"

→ make-prompts speech --with-history → parallel_tasks
→ make-prompts vote --with-history → parallel_tasks
→ sheriff-direction 左|右
→ day-auto --speech ... --vote ...
→ complete_step "白天发言+投票+处决"
```

### 模板 3：后续白天（第 2 天起）

```
todo_write [
  {content:"白天（发言→投票→处决）", status:in_progress}
]

→ make-prompts speech --with-history → parallel_tasks
→ make-prompts vote --with-history → parallel_tasks
→ sheriff-direction 左|右（如有警长）
→ day-auto --speech ... --vote ...
→ complete_step "白天（发言→投票→处决）"
```

## 更多信息

游戏规则、命令详情、配置、策略文件、模板 → 读 `REFERENCE.md`。
