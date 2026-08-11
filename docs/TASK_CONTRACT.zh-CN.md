# 任务合约与暂停策略

<a href="./GUIDE.zh-CN.md">使用指南</a>
&nbsp;·&nbsp;
<a href="./TASK_CONTRACT.md">English</a>

复杂任务最适合写成一份**任务合约**：背景是什么、要完成什么动作、结果怎么交付、哪些边界不能越过，以及什么时候才需要暂停问用户。
有些 prompt 模板把最后一段叫 “Checkpoint”；Reasonix 文档里叫 “Pause policy”，避免和 Checkpoints/Rewind 快照混淆。

这不是更长的角色设定。更强的 coding agent 通常不需要用户教它一步步思考；它更需要清楚的任务边界和验收标准。

## 模板

```text
Context:
我正在做 [大任务]。
目标对象是 [谁]。
这个结果要帮助他们 [达成什么结果]。

Request:
请完成 [一个明确动作]。

Output format:
请按照 [具体结构] 输出。
必须包含 [必要模块]。
不要超过 [长度/范围]。

Constraints:
不要 [错误假设]。
不要 [越界内容]。
不要 [低质量输出形式]。
如果 [信息不足]，请明确标注不确定性。

Pause policy:
除非下一步涉及不可逆或对外可见操作、任务范围变化，或需要我提供信息，否则请继续完成任务后再汇报。
```

## Reasonix 如何使用它

- **普通聊天**可以直接粘贴这份模板，适合一次性任务。
- **Goal 模式**会把目标当作任务合约持续推进，直到 request、output format、constraints 和必要验证都满足。
- **计划模式**适合“先产出并确认方案，再进入实施”的场景；它是工作流指令，不是只读权限边界。
- **工具审批**仍然独立生效：写文件、跑 shell、发布、凭证、外部副作用都会继续遵守配置的审批策略。
- **Checkpoints/Rewind** 是代码和会话快照；这里的暂停策略只描述 agent 什么时候应该停下来问用户。

Goal 模式里的任务合约会随 user turn 注入 provider 可见上下文，不会改写 cache-stable system prompt、memory prefix 或 tool schema。

## 示例

```text
/goal Context:
我正在改进桌面端输入框。
目标对象是需要连续做代码审查的用户。
这个结果要帮助他们避免被补全菜单打断输入。

Request:
修复 slash-command 菜单打开时键盘焦点丢失的问题。

Output format:
完成后按“改了什么 / 验证结果 / 剩余风险”汇报。

Constraints:
不要改变 Wails JSON 合约。
不要顺手重构无关的 composer 状态。
如果无法跑浏览器验证，请说明原因。

Pause policy:
除非下一步需要产品判断、公开 push 或凭证，否则继续完成实现和验证后再汇报。
```
