# 狼人杀参考手册

引擎路径：`.reasonix/skills/werewolf/scripts/werewolf_game.py`

调用格式：
```bash
cd /c/Users/zhujieling11/goal-test && python3 .reasonix/skills/werewolf/scripts/werewolf_game.py <命令>
```

---

## 目录

- [游戏规则](#游戏规则)
- [主持循环](#主持循环)
- [命令速查](#命令速查)
- [配置系统](#配置系统)
- [Prompt 指南](#prompt-指南)
- [人格系统](#人格系统)
- [硬性规则](#硬性规则)
- [GM 工作流](#gm-工作流)

---

## 游戏规则

### 角色

| 角色 | 数量 | 能力 |
|------|------|------|
| 狼人 | 3 | 每晚共同刀一人，认识队友 |
| 狼王 | 1 | 被投后带走任意一人 |
| 村民 | 3 | 白天投票处决狼人 |
| 预言家 | 1 | 每晚查验一人的真实身份 |
| 女巫 | 1 | 解药救被刀的人，毒药毒人；同守同救=死 |
| 猎人 | 1 | 被处决/被狼刀时可开枪带走一人；被毒死不能开枪 |
| 守卫 | 1 | 每晚守护一人不被狼刀，不能连续两晚守同一人 |
| 白痴 | 1 | 被处决时翻牌不死，失去投票权但可继续发言 |

### 特殊规则

- **同守同救**：守卫和女巫同夜保护同一人 → 该人仍死亡
- **白痴翻牌**：被投票处决时亮明身份，之后不能投票但可发言
- **猎人开枪**：被毒死不能开枪。被狼刀/被投死时可开枪
- **狼自爆**：白天投票前自爆 → 跳过当天投票（需配置 `wolf_explode=true`）
- **猎人主动开枪**：白天可亮身份开枪，猎人自己死亡（需配置 `hunter_active_shot=true`）

### 胜利条件

- **好人阵营**：消灭所有狼人
- **狼人阵营**：狼人数 ≥ 好人数（屠城）

---

## 回复格式规范

**原则：决策需要理由（1句话），投票保持简洁**

| 阶段 | 格式要求 | 示例 |
|------|----------|------|
| 守卫守护 | `名字 + 1句话理由` | `张鹏，首夜空过` |
| 女巫救人 | `救/不救 + 1句话理由` | `救，首夜必救` |
| 女巫毒人 | `名字/跳过 + 1句话理由` | `跳过，没信息` |
| 预言家查验 | `名字 + 1句话理由` | `张鹏，第一个发言的` |
| 狼队提案 | `刀谁 + 想悍跳/倒钩 + 1句话` | `刀张鹏，我想悍跳查杀李强` |
| 狼队讨论 | 自由讨论，≤100字 | |
| 狼队分工 | `刀X + 悍跳Y + 倒钩Z` | `刀张鹏 + 悍跳李强 + 倒钩王五` |
| 投票 | `名字` | `张鹏` |
| 上警 | `上/不上` | `上` |
| 退水 | `退/不退` | `不退` |
| 遗言 | 自由发言，≤150字 | |
| 猎人开枪 | `带谁/不开 + 1句话理由` | `带张鹏，他是狼` |
| 白痴翻牌 | `翻/不翻` | `翻` |

---

## 主持循环

```
初始化 → 第0晚 → 第1天 → 第1晚 → 第2天 → ... → 胜负判定
```

### 夜间流程（13个阶段）

详细触发顺序见 `scripts/skill-trigger-order.md`

**⚠️ 狼队讨论必须完整执行4个阶段，不能跳过任何阶段！**

**阶段1：守卫行动**
```bash
make-prompts night_guard
task 问守卫守护谁（不能连守同一人）
格式：名字 + 1句话理由（如：张鹏，首夜空过）
```

**阶段2a：狼队独立提案**
```
make-prompts wolf_strategy → parallel_tasks 问每狼独立提案
格式：刀谁 + 想悍跳/倒钩 + 1句话（如：刀张鹏，我想悍跳查杀李强）
```

**阶段2b：狼队集体讨论（必须执行！）**
```
parallel_tasks 让所有狼看到彼此提案，根据当前版面决定大致战术，讨论分歧
每狼输出：同意/反对/修改建议
格式：自由讨论，≤100字
```

**阶段2c：狼队投票决策（有分歧时必须执行！）**
```
parallel_tasks 投票决定最终方案
每狼输出：支持方案A/B/C
格式：支持方案A/B/C
```

**阶段2d：狼队分工确认**
```
task 确认最终分工：谁悍跳 + 谁倒钩 + 刀谁 + 查杀目标
格式：刀X + 悍跳Y + 倒钩Z
```

**阶段3a：女巫救人**
```
有解药 → task 问救不救（只问救，不问毒）
格式：救/不救 + 1句话理由（如：救，首夜必救）
救了 → 跳过阶段3b
没救 / 解药用过 → 进入阶段3b
```

**阶段3b：女巫毒人**
```
task 问毒不毒（只问毒，不问救）
格式：名字/跳过 + 1句话理由（如：跳过，没信息）
```

**阶段4：预言家查验**
```bash
make-prompts night_check
task 问预言家验谁
格式：名字 + 1句话理由（如：张鹏，第一个发言的）
```

**阶段5：执行夜间+死亡判定**
```bash
night-auto --wolf "狼:刀X" --guard "守Y" --seer "查Z" --witch "救X"
```

**阶段6：死亡后续**
```
有猎人被刀 → 问是否开枪（hunter_active prompt）
格式：带谁/不开 + 1句话理由（如：带张鹏，他是狼）
有警长死亡 → 问传给谁
有遗言 → 收集遗言（make-prompts last_words）
格式：自由发言，≤150字
```

### 白天流程（6个阶段）

**阶段1：确认局面**

```

**阶段2：警长方向（如有警长）**
```bash
sheriff-direction 左|右
```

**阶段3：收集发言**
```bash
make-prompts speech --with-history
parallel_tasks 让所有存活玩家按顺序发言
格式：自由发言，≤200字
```

**阶段4：收集投票**
```bash
make-prompts vote --with-history
parallel_tasks 让所有存活玩家投票
格式：名字（如：张鹏）
```

**阶段5：处决前检查**
```
被处决者如果是白痴 → 问是否翻牌（day-idiot-reveal prompt）
格式：翻/不翻
被处决者如果是猎人/狼王 → 问是否开枪（hunter_active/wolf_explode prompt）
格式：带谁/不开 + 1句话理由
被处决者如果是警长 → 问传给谁
格式：名字
```

**阶段6：执行处决**
```bash
day-auto --speech "回复" --vote "回复"
```

**阶段7：死亡后续**
```
有猎人开枪 → 执行开枪（day-auto --hunter）
有警长死亡 → 执行传位
有遗言 → 收集遗言（make-prompts last_words）
格式：自由发言，≤150字
```

### 警长竞选（6个阶段，仅第1天）

**阶段1：收集上警意愿**
```
parallel_tasks 问每个存活玩家：是否上警？
格式：上 / 不上
```

**阶段2：候选人发言**
```
task 问每个上警候选人发言（报查验+警徽流，≤150字）
候选人列表 = 阶段1选"上"的玩家
非候选人此时不发言
```

**阶段3：退水确认（必须执行！）**
```
make-prompts sheriff_withdraw → task 问每个候选人：是否退水？
格式：退/不退 + 一句话理由
退水后恢复投票权，可在警下投票
候选人列表 = 退水后剩余的候选人
```

**阶段4：确认投票权**
```
⚠️ 关键步骤！退水玩家恢复投票权！
投票权玩家 = 存活玩家 - 当前候选人列表
```

**阶段5：警下投票**
```
parallel_tasks 收集投票权玩家的投票
格式：名字（如：张鹏）
⚠️ 候选人不能投票给自己
```

**阶段6：执行**
```bash
sheriff --candidates A B C --vote "D:A" "E:B" ...
```
→ 有人超半数 → 当选
→ 平票 → PK轮（阶段2-6重复，只保留平票者）
→ 再平 → **警徽流失**
→ 全部退水 → `sheriff --candidates`（不加参数）→ 警徽流失

---

## 命令速查

| 命令 | 用途 | 说明 |
|------|------|------|
| `init 名1 名2 ...` | 初始化游戏 | 自动按人数分配角色 |
| `night-auto --wolf --guard --seer --witch` | 自动夜间 | 接收 AI 原始回复，自动执行 |
| `day-auto --speech --vote --hunter` | 自动白天 | 警长存活需先用 sheriff-direction 确定方向 |
| `sheriff --candidates --vote` | 警上竞选 | 候选人→投票→当选/流失 |
| `sheriff-direction 左|右` | 警长方向 | 警长确定发言起始方向 |
| `summary [--for-player X] [--with-history]` | 安全状态摘要 | --for-player 只显示该玩家应知信息 |
| `make-prompts <action>` | 生成 prompt | 自动为所有玩家生成完整prompt |
| `save-wolf-plans <玩家> --strategy --claim --check` | 保存狼人策略 | 悍跳/倒钩/深水/冲锋 + 查杀/金水目标 |
| `replay [--output 文件名]` | 生成对局复盘 | 默认 replay.md |
| `status-pretty [--roles]` | 干净状态 | 显示模仿目标 |
| `journal` | 本局战报 | 角色表+死亡线+处决记录 |
| `stats` | 胜率统计 | 累计对局记录 |
| `hint <角色>` | 策略提示 | 当前局势下的角色建议 |
| `config [--show] [key value]` | 配置 | 查看/修改规则开关 |
| `reset` | 重置 | 清空游戏状态 |

---

## 配置系统

通过 `werewolf_config.json` 管理所有规则开关：

```bash
python3 .reasonix/skills/werewolf/scripts/werewolf_game.py config --show
python3 .reasonix/skills/werewolf/scripts/werewolf_game.py config wolf_explode true
```

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `witch_self_save_n1` | true | 女巫首夜可以救自己（标准规则） |
| `guard_witch_overlap_lethal` | false | 同守同救是否致死 |
| `wolf_self_kill` | true | 允许狼人刀队友/自刀（标准规则） |
| `wolf_explode` | false | 狼人可以在白天自爆 |
| `hunter_active_shot` | false | 允许猎人主动开枪 |
| `mechanical_wolf` | false | 启用机械狼 |
| `mimic_wolf` | false | 机械狼变为模仿狼 |
| `reveal_role_on_death` | false | 死亡时显示具体身份 |

角色开关：`role_seer`、`role_witch`、`role_hunter`、`role_guard`、`role_idiot`（均默认 true）

**常见规则组合：**
- 标准局：`witch_self_save_n1=true` + `wolf_self_kill=true`
- 首夜不可自救：`witch_self_save_n1=false`
- 禁止自刀：`wolf_self_kill=false`

---

## Prompt 指南

### 自动生成 Prompt

```bash
# 基础用法
python3 .reasonix/skills/werewolf/scripts/werewolf_game.py make-prompts speech --with-history
python3 .reasonix/skills/werewolf/scripts/werewolf_game.py make-prompts vote --with-history

# 指定玩家数量（用于策略差异化）
python3 .reasonix/skills/werewolf/scripts/werewolf_game.py make-prompts sheriff --player-count 12

# 新增 action 类型
python3 .reasonix/skills/werewolf/scripts/werewolf_game.py make-prompts last_words
python3 .reasonix/skills/werewolf/scripts/werewolf_game.py make-prompts hunter_active
python3 .reasonix/skills/werewolf/scripts/werewolf_game.py make-prompts idiot_reveal
python3 .reasonix/skills/werewolf/scripts/werewolf_game.py make-prompts wolf_explode
python3 .reasonix/skills/werewolf/scripts/werewolf_game.py make-prompts mimic

# 狼人策略调整（第2天+自动启用历史信息）
python3 .reasonix/skills/werewolf/scripts/werewolf_game.py make-prompts wolf_adjust
python3 .reasonix/skills/werewolf/scripts/werewolf_game.py make-prompts night_kill --with-history
```

### 狼人策略调整机制

| 角色 | 核心任务 | 调整时机 |
|------|---------|---------|
| 悍跳狼 | 跳预言家，查杀/金水 | 被怀疑时转为倒钩 |
| 冲锋狼 | 用逻辑放大真预问题 | 安全时继续冲锋 |
| 倒钩狼 | 站边真预，做身份 | 被查杀时转为冲锋 |
| 深水狼 | 活到最后，不被抗推 | 上焦点位时转为冲锋 |

### 新增行动类型

| 行动 | 命令 | 策略注入 |
|------|------|---------|
| 首夜分工+刀人提案 | `make-prompts wolf_strategy` | `wolf/core.md` + `wolf/hunting/godsniff.md` |
| 悍跳专用策略 | `make-prompts wolf_claim` | `wolf/claim.md` |
| 夜间找神 | `make-prompts wolf_hunting` | `wolf/hunting/godsniff.md` |
| 深水狼策略 | `make-prompts wolf_deep` | `wolf/core.md` |
| 中局调整 | `make-prompts wolf_adjust` | `wolf/tactics.md` + `wolf/hunting/godsniff.md` |


`make-prompts` 支持 `--player-count` 参数：

| 人数 | 上警建议 | 狼人上警 | 神职策略 |
|------|---------|---------|---------|
| 6-7人 | 2人上警 | 1狼上警 | 隐藏为主 |
| 8-9人 | 3人上警 | 1-2狼上警 | 灵活选择 |
| 10-12人 | 4-5人上警 | 2狼上警 | 标准打法 |

### 手动构建 Prompt

从 `scripts/prompts/` 目录读取模板，按角色填充变量：

| 场景 | 引用文件 |
|------|---------|
| 狼人夜间 | `scripts/prompts/night-wolf.md` |
| 女巫夜间 | `scripts/prompts/night-witch.md` |
| 预言家夜间 | `scripts/prompts/night-seer.md` |
| 守卫夜间 | `scripts/prompts/night-guard.md` |
| 白天发言 | `scripts/prompts/day-speech.md` |
| 白天投票 | `scripts/prompts/day-vote.md` |
| 警长竞选 | `scripts/prompts/sheriff.md` |
| 遗言阶段 | `scripts/prompts/last-words.md` |
| 猎人主动开枪 | `scripts/prompts/day-hunter-active.md` |
| 白痴翻牌 | `scripts/prompts/day-idiot-reveal.md` |
| 狼人自爆 | `scripts/prompts/wolf-explode.md` |
| 机械狼夜间 | `scripts/prompts/night-mimic.md` |

### 策略文件

| 场景 | 引用文件 |
|------|---------|
| 技能触发顺序 | `scripts/skill-trigger-order.md` |
| 狼人核心理念 | `scripts/strategies/wolf/core.md` |
| 狼人悍跳 | `scripts/strategies/wolf/claim.md` |
| 狼人战术 | `scripts/strategies/wolf/tactics.md` |
| 狼人抿神 | `scripts/strategies/wolf/hunting/godsniff.md` |
| 好人通用 | `scripts/strategies/good/general.md` |
| 挡刀 | `scripts/strategies/good/deflect.md` |
| 躲刀 | `scripts/strategies/good/god_hide.md` |
| 神职 | `scripts/strategies/good/god.md` |
| 大局观 | `scripts/strategies/overview.md` |
| 不见面分析 | `scripts/strategies/topics/nonmeet/nonmeet.md` |
| 语境分析 | `scripts/strategies/topics/context/context.md` |
| 博弈论 | `scripts/strategies/topics/gametheory.md` |
| 决胜局策略 | `scripts/strategies/topics/final-battle.md` |
| 贝叶斯 | `scripts/strategies/topics/bayesian.md` |

---

## 人格系统

| 人格 | 语气 | 逻辑模式 | 发言长度 |
|------|------|---------|---------|
| 分析家 🔍 | 理性、简洁 | 视角差+不见面关系+收益分析 | 150-300字 |
| 演说家 🎤 | 有起伏、讲故事 | 抛结论→举例子→反问拉拢 | 200-400字 |
| 公务员 📋 | 首先/其次/最后 | 列条款式 | 150-300字 |
| 愣头青 💥 | 直接、不怕得罪 | 直觉判断+情绪输出 | 100-200字 |

---

## 硬性规则

### 每个 task prompt 必须包含状态上下文

**必须**在 prompt 开头粘贴状态信息（用 `make-prompts <action> --with-history` 生成，已自动包含上下文）：

```
═══ 第2天 ══ 存活7人
├ 存活: 甲 乙 丙 丁 戊 己 庚
├ 死亡: 辛 壬
├ 事件:
│ 狼人刀了 辛
│ 女巫救了 壬
│ 处决了 辛

你是【狼人】。存活：【甲 乙 丙 丁 戊 己 庚】。
队友：【庚】。
昨天：辛被处决。任务：今天投谁？只需名字。
```

### 检查清单

- [ ] 存活玩家列表给了吗？
- [ ] 昨夜/昨天结果给了吗？
- [ ] 该玩家自己的角色给了吗？
- [ ] 如果是狼人，队友给了吗？

### 防幻觉

- ✅ 当前是9人局，3狼+预言家+女巫+猎人+3村民。没有守卫，没有白痴。
- ❌ 不要说"12人预女猎白标准局"，AI会照搬12人模板。

### 防止预言家乱跳

1. 真预言家：`你是【预言家】。你是真正的预言家！`
2. 非预言家：`你是【女巫】【角色名】。你不是预言家！`
3. 狼人可以悍跳预言家；好人可以不跳或穿神衣服

---

## GM 工作流

### 白天标准操作（7个阶段）

```
阶段1：bash: sheriff-direction 左|右（如有警长）
阶段2：bash: make-prompts speech --with-history → parallel_tasks 收集发言
  格式：自由发言，≤200字
阶段3：bash: make-prompts vote --with-history → parallel_tasks 收集投票
  格式：名字（如：张鹏）
阶段4：bash: 处决前检查（白痴/猎人/狼王/警长传位）
  白痴：翻/不翻
  猎人/狼王：带谁/不开 + 1句话理由
  警长传位：名字
阶段5：bash: day-auto --speech "回复" --vote "回复"
阶段6：bash: 处理死亡后续（猎人开枪/警长传位/遗言）
  遗言：自由发言，≤150字
```

### 夜间标准操作（13个阶段）

```
阶段1：bash: make-prompts night_guard → task 守卫行动
  格式：名字 + 1句话理由（如：张鹏，首夜空过）
阶段2a：bash: make-prompts wolf_strategy → parallel_tasks 独立提案
  格式：刀谁 + 想悍跳/倒钩 + 1句话
阶段2b：bash: parallel_tasks 集体讨论（必须执行！）
  格式：自由讨论，≤100字
阶段2c：bash: (有分歧) parallel_tasks 投票决策（必须执行！）
  格式：支持方案A/B/C
阶段2d：bash: task 分工确认
  格式：刀X + 悍跳Y + 倒钩Z
阶段3a：bash: task 女巫救人（有解药才问）
  格式：救/不救 + 1句话理由
阶段3b：bash: task 女巫毒人（救人没成功才问）
  格式：名字/跳过 + 1句话理由
阶段4：bash: make-prompts night_check → task 预言家查验
  格式：名字 + 1句话理由
阶段5：bash: night-auto --wolf --guard --seer --witch
阶段6：bash: 处理死亡后续（猎人开枪/警长传位/遗言）
  猎人：带谁/不开 + 1句话理由
  遗言：自由发言，≤150字
```

### 警长竞选标准操作（6个阶段）

```
阶段1：parallel_tasks 收集上警意愿
  格式：上/不上
阶段2：task 候选人发言
  格式：自由发言，≤150字（报查验+警徽流）
阶段3：make-prompts sheriff_withdraw → task 退水确认（必须执行！）
  格式：退/不退 + 一句话理由
  退水后恢复投票权，可在警下投票
阶段4：⚠️ 确认投票权（退水玩家恢复投票权）
阶段5：parallel_tasks 警下投票
  格式：名字（如：张鹏）
阶段6：bash: sheriff 执行 → (平票) PK轮 → (再平) 警徽流失
```

### 关键技巧

- **缓存优化**：同轮所有子 agent 的 prompt 前缀相同（`summary` 输出），第二个起触发缓存命中
- **方向确定**：有警长时先问警长发言方向（`sheriff-direction 左|右`），再执行 `day-auto`
- **警长竞选失败**：第1天警长竞选平票→PK→再平导致警徽流失，之后 `day-auto --no-sheriff`
- **警长夜间死亡**：`night-auto` 会自动检测警长死亡并要求传位；加 `--no-sheriff-confirm` 可跳过
- **记录 AI 原始回复**（调试用）：

  ```bash
  log-raw speech 深哥 "我站边大克，他是真预言家"
  log-raw vote 深哥 "投大克"
  log-raw wolf_kill 深哥 "刀京爷"
  ```

- **超时处理**：如果某个子 agent 决策长时间未返回，直接默认合理选择推进
- **严禁自行发明规则**：所有规则以 `werewolf_config.json` 为准
- **死预言家不再查验**：死亡预言家不会在夜间继续查验
