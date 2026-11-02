团队：一组长期在的 Agent，可以互相派活、在群聊里轮流协作。

## 新增

- 侧栏的团队区块：主 Agent（Chief）、你建的 Agent 和群聊，按最近活动排序。 #14623
- 团队和项目可通过相同的标题/箭头折叠整个分区，保留筛选与各项目的展开状态；展开/收起所有项目移至项目的更多菜单。 #14623
- 每个 Agent 有自己的名字、角色、指令、头像和长期对话，详情抽屉里看模型与用量。 #14623
- Agent 之间用 message_agent 派活，回复自动回到发起方，对话里可以展开往来。 #14623
- 群聊：点名、交给下一位、接力上限后暂停等你继续、停止某个成员、往前翻页。 #14623
- 三层记忆（Agent、团队、群）；自动整理默认关闭，开启后仅以用户消息作为新证据，并显示变更结果。 #14623
- 给 Agent 或群设定时任务；任务标注为未验证的调度输入并保留审批上限，错过的时段不补跑。 #14623
- Agent 可以向你要密钥：明确选择授权成员，命令引用变量时才注入；值保存在团队目录外的本机未加密凭据文件中，发给模型时显示为 [secret:名称]。首次登记若命中旧历史，可能产生一次提示词缓存未命中。 #14623
- Windows 暂不运行团队会话，普通会话照常可用。 #14623
- 在 Agent 的对话里 @ 另一个 Agent 或引用它的对话；终端里用 /team 和 /msg。 #14623
- 用量页按 Agent 统计；每个 Agent 的浏览器用自己的登录状态。 #14623
- 按 Agent 用量与未定价调用原因可同时显示；升级时重建本地用量缓存，保留原始记录。 #14623

<!-- lang:en -->

Team: long-lived agents that hand work to each other and take turns in group chats.

## New

- A Team block in the sidebar: Chief, the agents you create and group chats, by latest activity. #14623
- Team and Projects share a title/chevron to fold the whole section while preserving filters and individual project folds. Expand/collapse all projects lives in the Projects more menu. #14623
- Each agent has a name, role, instructions, avatar and long-lived chat; a drawer shows model and usage. #14623
- Agents hand work on with message_agent; replies return to the sender and expand in the chat. #14623
- Group chats: mention, hand on, pause at the relay limit until you continue, stop a member, page back. #14623
- Three tiers of memory (agent, team, group). Automatic review is opt-in, uses human messages as new evidence and reports changes. #14623
- Schedules for an agent or group retain unverified scheduler provenance and an approval ceiling. Missed slots are skipped. #14623
- Secrets require explicit recipients and reach only commands that reference their variables. Models see [secret:NAME]. #14623
- Secret values live outside the team directory in the unencrypted local credential file. First registration may rewrite matching history and cause one prompt-cache miss. #14623
- Team execution is temporarily disabled on Windows; ordinary sessions remain available. #14623
- Mention another agent or quote its chat from an agent's chat; /team and /msg in the terminal. #14623
- Usage by agent on the usage page; each agent's browser keeps its own logins. #14623
- Agent usage and unpriced-call reasons remain available together. Upgrading rebuilds the local usage cache while preserving the original records. #14623
