package i18n

// FeedbackText is what /feedback says about the install's standing and about a
// refusal that names its window. Level names are keyed by the level number the
// service reports, limit names by the window identifier it reports; "" in
type FeedbackText struct {
	LevelNames          map[string]string
	LevelNextFmt        string // level, name, shipped, still needed, next name
	LevelTopFmt         string // level, name, shipped
	LimitsFmt           string // reports an hour, reports a day, replies an hour
	StandingStale       string // appended while the service is unreachable
	TrustLegacyFmt      string // the date the higher limits end
	TrustLapsed         string
	LimitNames          map[string]string
	LimitUnknown        string
	RefusedResetsFmt    string // limit name, local reset time
	RefusedLaterFmt     string // limit name
	RefusedPermanentFmt string // limit name
}

var feedbackEnglish = FeedbackText{
	LevelNames: map[string]string{
		"0": "New seed", "1": "Sprout", "2": "Seedling", "3": "Sapling", "4": "Bloom", "5": "Canopy", "6": "Grove",
	},
	LevelNextFmt:   "Level %d, %s: %d shipped, %d more to reach %s.",
	LevelTopFmt:    "Level %d, %s: %d shipped, the highest level.",
	LimitsFmt:      "Limits now: %d reports an hour, %d a day, %d replies an hour.",
	StandingStale:  "(last confirmed - the service is unreachable)",
	TrustLegacyFmt: "Your current higher limits stay until %s.",
	TrustLapsed:    "Your higher limits have lapsed; your level and count are kept, and a later shipped outcome renews them.",
	LimitNames: map[string]string{
		"ip_hourly":      "the hourly limit for this network",
		"install_hourly": "the hourly report limit",
		"install_daily":  "the daily report limit",
		"reply_hourly":   "the hourly reply limit",
		"reply_item":     "the reply limit for this report",
		"global_daily":   "the service's daily capacity",
		"global_burst":   "the service's short-term capacity",
	},
	LimitUnknown:        "a limit",
	RefusedResetsFmt:    "not sent: %s was reached. It resets at %s.",
	RefusedLaterFmt:     "not sent: %s was reached. Try again later.",
	RefusedPermanentFmt: "not sent: %s was reached; it does not reset.",
}

var feedbackChinese = FeedbackText{
	LevelNames: map[string]string{
		"0": "新种", "1": "萌芽", "2": "幼苗", "3": "小树", "4": "繁花", "5": "成荫", "6": "共林",
	},
	LevelNextFmt:   "等级 %d「%s」：已落地 %d 条，再落地 %d 条升到「%s」。",
	LevelTopFmt:    "等级 %d「%s」：已落地 %d 条，已是最高等级。",
	LimitsFmt:      "当前额度：每小时 %d 条反馈、每天 %d 条、每小时 %d 条回复。",
	StandingStale:  "（上次确认的结果，反馈服务暂时连不上）",
	TrustLegacyFmt: "你现在较高的额度保留到 %s。",
	TrustLapsed:    "你的较高额度已经失效；等级和计数都保留，之后再有反馈落地就会续上。",
	LimitNames: map[string]string{
		"ip_hourly":      "这个网络的每小时上限",
		"install_hourly": "每小时反馈次数上限",
		"install_daily":  "每日反馈次数上限",
		"reply_hourly":   "每小时回复次数上限",
		"reply_item":     "这份反馈的回复次数上限",
		"global_daily":   "服务今天的总接收量",
		"global_burst":   "服务短时间内的接收量",
	},
	LimitUnknown:        "某个上限",
	RefusedResetsFmt:    "未发送：已达到%s，将在 %s 重置。",
	RefusedLaterFmt:     "未发送：已达到%s，请稍后再试。",
	RefusedPermanentFmt: "未发送：已达到%s，它不会自动重置。",
}

var feedbackTraditional = FeedbackText{
	LevelNames: map[string]string{
		"0": "新種", "1": "萌芽", "2": "幼苗", "3": "小樹", "4": "繁花", "5": "成蔭", "6": "共林",
	},
	LevelNextFmt:   "等級 %d「%s」：已落地 %d 條，再落地 %d 條升到「%s」。",
	LevelTopFmt:    "等級 %d「%s」：已落地 %d 條，已是最高等級。",
	LimitsFmt:      "目前額度：每小時 %d 條回饋、每天 %d 條、每小時 %d 條回覆。",
	StandingStale:  "（上次確認的結果，回饋服務暫時連不上）",
	TrustLegacyFmt: "你現在較高的額度保留到 %s。",
	TrustLapsed:    "你的較高額度已經失效；等級和計數都保留，之後再有回饋落地就會續上。",
	LimitNames: map[string]string{
		"ip_hourly":      "這個網路的每小時上限",
		"install_hourly": "每小時回饋次數上限",
		"install_daily":  "每日回饋次數上限",
		"reply_hourly":   "每小時回覆次數上限",
		"reply_item":     "這份回饋的回覆次數上限",
		"global_daily":   "服務今天的總接收量",
		"global_burst":   "服務短時間內的接收量",
	},
	LimitUnknown:        "某個上限",
	RefusedResetsFmt:    "未傳送：已達到%s，將在 %s 重置。",
	RefusedLaterFmt:     "未傳送：已達到%s，請稍後再試。",
	RefusedPermanentFmt: "未傳送：已達到%s，它不會自動重置。",
}
