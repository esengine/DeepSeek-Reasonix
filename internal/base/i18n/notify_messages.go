package i18n

// NotifyMessages is the text of every desktop notification; Messages embeds it,
// so a new notification kind adds a line here and not to the catalogue struct.
type NotifyMessages struct {
	NoticeInboxUnrecovered string // the inbox bodies could not be read, so the queue is paused with unknown contents
	NotifyTitle            string // desktop notification title
	NotifyTurnDone         string // the turn finished
	NotifyTurnFailed       string // the turn failed
	NotifyApproval         string // something is waiting to be approved
	NotifyAsk              string // the model asked a question
	NotifyFeedbackReply    string // a feedback report has a new reply
	NotifyDiskLow          string // a volume is nearly full; formatted with the free space, then the folder's name
	AskTimeoutMessages
	TeamCommandMessages
	UnattendedMessages
	PaidSearchMessages
	MCPSignInMessages
}

// AskTimeoutMessages is the /ask-timeout command's text.
type AskTimeoutMessages struct {
	CmdAskTimeout    string // the command's hint
	AskTimeoutFmt    string // status line, one %s state
	AskTimeoutOff    string // the word for the timeout being off
	AskTimeoutBadFmt string // argument that is neither a number nor off, one %s
	AskSkipFmt       string // the open question's countdown, one %s span
}

// PaidSearchMessages is the consent card for a search backend the vendor
// charges for.
type PaidSearchMessages struct {
	PaidSearchApprovalReasonFmt string // %[1]s backend, %[2]s price, %[3]s query
	PaidFetchApprovalReasonFmt  string // %[1]s backend, %[2]s price, %[3]s address
}

// MCPSignInMessages is the consent card shown before a browser opens for an MCP
// server's sign-in.
type MCPSignInMessages struct {
	MCPSignInReasonFmt string // %[1]s server, %[2]s authorization host
	// MCPScopeUpgradeReasonFmt asks for more access than the user granted: %[1]s server, %[2]s host, %[3]s the scopes it named.
	MCPScopeUpgradeReasonFmt string
	// McpPanelSignIn and McpPanelUpgradeFmt are the /mcp panel hints for the sign-in and upgrade keys.
	McpPanelSignIn, McpPanelUpgradeFmt string
}
