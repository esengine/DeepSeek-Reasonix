package i18n

// TeamCommandMessages is the /team and /msg commands' text.
type TeamCommandMessages struct {
	CmdTeam            string // /team's hint
	CmdMsg             string // /msg's hint
	TeamReadErrorFmt   string // the team could not be read, one %s error
	TeamEmpty          string // no agents yet
	TeamAgentsHeading  string // above the agents
	TeamGroupsHeading  string // above the group chats
	TeamMain           string // marks a main agent
	TeamMsgHint        string // how to send one a message
	MsgUsage           string // /msg without a name and a message
	MsgUnknownAgentFmt string // no agent by that name, one %s name
	MsgQueuedFmt       string // queued for an agent, one %s name
}

var teamCmdEN = TeamCommandMessages{
	CmdTeam:            "list the agents and group chats on your team",
	CmdMsg:             "send an agent on your team a message: /msg <name> <message>",
	TeamReadErrorFmt:   "team: could not read it: %s",
	TeamEmpty:          "Your team has no agents yet. Create Chief in Reasonix Studio's sidebar.",
	TeamAgentsHeading:  "Agents:",
	TeamGroupsHeading:  "Group chats:",
	TeamMain:           "main",
	TeamMsgHint:        "Send one a message with /msg <name> <message>.",
	MsgUsage:           "usage: /msg <name> <message>",
	MsgUnknownAgentFmt: "msg: no agent on your team is called %q — /team lists them",
	MsgQueuedFmt:       "sent to %s; it arrives within seconds while Reasonix Studio is open, or the next time it opens",
}

var teamCmdZH = TeamCommandMessages{
	CmdTeam:            "列出团队里的 Agent 和群聊",
	CmdMsg:             "给团队里的 Agent 发消息：/msg <名字> <消息>",
	TeamReadErrorFmt:   "团队：读不出来：%s",
	TeamEmpty:          "团队里还没有 Agent。在 Reasonix Studio 的侧栏里创建 Chief。",
	TeamAgentsHeading:  "Agent：",
	TeamGroupsHeading:  "群聊：",
	TeamMain:           "主 Agent",
	TeamMsgHint:        "用 /msg <名字> <消息> 给其中一个发消息。",
	MsgUsage:           "用法：/msg <名字> <消息>",
	MsgUnknownAgentFmt: "msg：团队里没有叫 %q 的 Agent — 用 /team 查看",
	MsgQueuedFmt:       "已发给 %s；Reasonix Studio 开着时几秒内送达，否则下次打开时送达",
}

var teamCmdZHTW = TeamCommandMessages{
	CmdTeam:            "列出團隊裡的 Agent 和群聊",
	CmdMsg:             "給團隊裡的 Agent 傳訊息：/msg <名字> <訊息>",
	TeamReadErrorFmt:   "團隊：讀不出來：%s",
	TeamEmpty:          "團隊裡還沒有 Agent。在 Reasonix Studio 的側欄裡建立 Chief。",
	TeamAgentsHeading:  "Agent：",
	TeamGroupsHeading:  "群聊：",
	TeamMain:           "主 Agent",
	TeamMsgHint:        "用 /msg <名字> <訊息> 給其中一個傳訊息。",
	MsgUsage:           "用法：/msg <名字> <訊息>",
	MsgUnknownAgentFmt: "msg：團隊裡沒有叫 %q 的 Agent — 用 /team 查看",
	MsgQueuedFmt:       "已傳給 %s；Reasonix Studio 開著時幾秒內送達，否則下次開啟時送達",
}
