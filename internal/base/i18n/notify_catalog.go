package i18n

// The text of every desktop notification, per language; each catalogue embeds its own.

var notifyEN = NotifyMessages{
	NoticeInboxUnrecovered: "The queued instructions could not be read, so the inbox contents are unknown and it came up paused. Nothing was discarded: check with /queue and inspect the files before anything overwrites them.",
	UnattendedMessages:     unattendedEN,
	TeamCommandMessages:    teamCmdEN,
	PaidSearchMessages:     PaidSearchMessages{PaidSearchApprovalReasonFmt: "web_search wants to send this query to DeepSeek's paid Search API: %[3]s. DeepSeek bills %[2]s. Allow it for %[1]s?", PaidFetchApprovalReasonFmt: "web_fetch could not read this page from here and wants DeepSeek's paid Fetch API to fetch it: %[3]s. DeepSeek bills %[2]s. Allow it for %[1]s?"},
	MCPSignInMessages:      MCPSignInMessages{MCPSignInReasonFmt: "The MCP server %[1]s wants to send you to %[2]s to sign in. Only continue if you recognise that site; nothing is sent there until you approve.", MCPScopeUpgradeReasonFmt: "The MCP server %[1]s asks for more access than you granted it: %[3]s. These names come from the server, not from Reasonix. Only continue if you want it to have that access; you will be sent to %[2]s to review and grant it.", McpPanelSignIn: "this server needs you to sign in: press a", McpPanelUpgradeFmt: "press u to review: this server asks for more access: %s (its own words)"},
	AskTimeoutMessages: AskTimeoutMessages{
		CmdAskTimeout: "skip a question nobody answers after a wait", AskTimeoutFmt: "ask timeout: %s — set it with /ask-timeout <seconds|off>", AskTimeoutOff: "off", AskTimeoutBadFmt: "ask-timeout: %q is neither a number of seconds nor off", AskSkipFmt: "skipped in %s — press any key to keep it",
	},
	NotifyTitle:         "Reasonix",
	NotifyTurnDone:      "The turn finished",
	NotifyTurnFailed:    "The turn failed",
	NotifyApproval:      "Waiting for your approval",
	NotifyAsk:           "The model asked you something",
	NotifyFeedbackReply: "Your feedback has a new reply",
	NotifyDiskLow:       "Disk space is low: %s left on the drive holding %s",
}

var notifyZH = NotifyMessages{
	NoticeInboxUnrecovered: "队列内容读不出来，因此待发送里有什么是未知的，已暂停等待检查。内容没有被丢弃：请用 /queue 查看，并在任何写入覆盖之前检查文件。",
	UnattendedMessages:     unattendedZH,
	TeamCommandMessages:    teamCmdZH,
	PaidSearchMessages:     PaidSearchMessages{PaidSearchApprovalReasonFmt: "web_search 想把这条搜索词发给 DeepSeek 的付费搜索接口：%[3]s。DeepSeek 按次计费：%[2]s。是否允许使用 %[1]s？", PaidFetchApprovalReasonFmt: "web_fetch 在本机读不到这个页面，想用 DeepSeek 的付费抓取接口来抓：%[3]s。DeepSeek 按次计费：%[2]s。是否允许使用 %[1]s？"},
	MCPSignInMessages:      MCPSignInMessages{MCPSignInReasonFmt: "MCP 服务器 %[1]s 想让你去 %[2]s 登录。只有认得这个站点时才继续；批准之前不会向它发送任何东西。", MCPScopeUpgradeReasonFmt: "MCP 服务器 %[1]s 要求比你授予它的更多权限：%[3]s。这些名称来自服务器，不是 Reasonix 写的。只有你愿意让它拥有这些权限时才继续；之后会带你到 %[2]s 查看并授予。", McpPanelSignIn: "这个服务器需要你登录：按 a", McpPanelUpgradeFmt: "按 u 查看并授权：这个服务器要求更多权限：%s（是它自己写的名称）"},
	AskTimeoutMessages: AskTimeoutMessages{
		CmdAskTimeout: "设置提问无人回答多久后自动跳过", AskTimeoutFmt: "提问等待超时：%s — 用 /ask-timeout <秒数|off> 修改", AskTimeoutOff: "关闭", AskTimeoutBadFmt: "ask-timeout：%q 既不是秒数也不是 off", AskSkipFmt: "%s 后自动跳过 — 按任意键保留",
	},
	NotifyTitle:         "Reasonix",
	NotifyTurnDone:      "这一轮结束了",
	NotifyTurnFailed:    "这一轮失败了",
	NotifyApproval:      "有操作在等你批准",
	NotifyAsk:           "模型问了你一个问题",
	NotifyFeedbackReply: "你的反馈有新回复",
	NotifyDiskLow:       "磁盘空间不足：存放 %[2]s 的磁盘只剩 %[1]s",
}

var notifyZHTW = NotifyMessages{
	NoticeInboxUnrecovered: "佇列內容讀不出來，因此待發送裡有什麼是未知的，已暫停等待檢查。內容沒有被丟棄：請用 /queue 查看，並在任何寫入覆蓋之前檢查檔案。",
	UnattendedMessages:     unattendedZHTW,
	TeamCommandMessages:    teamCmdZHTW,
	PaidSearchMessages:     PaidSearchMessages{PaidSearchApprovalReasonFmt: "web_search 想把這條搜尋詞傳給 DeepSeek 的付費搜尋介面：%[3]s。DeepSeek 按次計費：%[2]s。是否允許使用 %[1]s？", PaidFetchApprovalReasonFmt: "web_fetch 在本機讀不到這個頁面，想用 DeepSeek 的付費抓取介面來抓：%[3]s。DeepSeek 按次計費：%[2]s。是否允許使用 %[1]s？"},
	MCPSignInMessages:      MCPSignInMessages{MCPSignInReasonFmt: "MCP 伺服器 %[1]s 想讓你去 %[2]s 登入。只有認得這個站點時才繼續；批准之前不會向它傳送任何東西。", MCPScopeUpgradeReasonFmt: "MCP 伺服器 %[1]s 要求比你授予它的更多權限：%[3]s。這些名稱來自伺服器，不是 Reasonix 寫的。只有你願意讓它擁有這些權限時才繼續；之後會帶你到 %[2]s 查看並授予。", McpPanelSignIn: "這個伺服器需要你登入：按 a", McpPanelUpgradeFmt: "按 u 查看並授權：這個伺服器要求更多權限：%s（是它自己寫的名稱）"},
	AskTimeoutMessages: AskTimeoutMessages{
		CmdAskTimeout: "設定提問無人回答多久後自動跳過", AskTimeoutFmt: "提問等待逾時：%s — 用 /ask-timeout <秒數|off> 修改", AskTimeoutOff: "關閉", AskTimeoutBadFmt: "ask-timeout：%q 既不是秒數也不是 off", AskSkipFmt: "%s 後自動跳過 — 按任意鍵保留",
	},
	NotifyTitle:         "Reasonix",
	NotifyTurnDone:      "這一輪結束了",
	NotifyTurnFailed:    "這一輪失敗了",
	NotifyApproval:      "有操作在等你核准",
	NotifyAsk:           "模型問了你一個問題",
	NotifyFeedbackReply: "你的回饋有新回覆",
	NotifyDiskLow:       "磁碟空間不足：存放 %[2]s 的磁碟只剩 %[1]s",
}
