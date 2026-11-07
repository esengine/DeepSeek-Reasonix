package i18n

var coldResumeEN = ColdResumeMessages{
	NoticeColdResumeFmt:        "Resumed after %s idle; the provider cache has likely expired. The full history is kept, and compaction waits for context pressure.",
	NoticeColdResumeCompactFmt: "Resumed after %s idle; the provider cache has likely expired, so the next request resends ~%s tokens uncached. /compact folds them first at no cache cost.",
}

var coldResumeZH = ColdResumeMessages{
	NoticeColdResumeFmt:        "闲置 %s 后续聊：服务商缓存很可能已过期。完整历史已保留，压缩会等到上下文接近上限时再进行。",
	NoticeColdResumeCompactFmt: "闲置 %s 后续聊：服务商缓存很可能已过期，下一次请求会不经缓存重发约 %s tokens。现在用 /compact 先压缩，不会损失缓存。",
}

var coldResumeZHTW = ColdResumeMessages{
	NoticeColdResumeFmt:        "閒置 %s 後續聊：服務商快取很可能已過期。完整歷史已保留，壓縮會等到上下文接近上限時再進行。",
	NoticeColdResumeCompactFmt: "閒置 %s 後續聊：服務商快取很可能已過期，下一次請求會不經快取重送約 %s tokens。現在用 /compact 先壓縮，不會損失快取。",
}
