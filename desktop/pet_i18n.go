//go:build darwin

package main

import (
	"encoding/json"
	"os"
	"strings"

	"reasonix/internal/i18n"
)

// PetI18n holds all localized strings for the pet UI.
type PetI18n struct {
	// State labels
	Idle    string
	Running string
	Waving  string
	Waiting string
	Failed  string
	Review  string
	Jumping string

	// Agent state bubbles
	RunningBubble   string
	WaitingBubble   string
	ToolErrorBubble string
	ErrorBubble     string

	// Greeting (turn started)
	Greetings []string
	// Task complete (running → idle)
	TaskDoneBubbles []string
	// Goodbye (all sessions ended)
	Goodbyes []string

	// Idle bubbles
	IdleBubbles []string

	// Click interactions
	HeadTexts []string
	BodyTexts []string

	// Drag
	DragIdleText string
	DragBusyText string

	// Double-click
	DblClickIdle0    string
	DblClickIdle1    string
	DblClickIdleMany string // %d sessions
	DblClickRunning  string // %d sessions
	DblClickWaiting  string // %d sessions
	DblClickReview   string // %d sessions
	DblClickFailed   string // %d sessions
	DblClickFallback string // sessions, state

	// Menu
	MenuTitle      string
	MenuSize       string
	MenuInstallPH  string
	MenuInstallBtn string
	MenuRandomBtn  string
	MenuMarketBtn  string

	// Install/delete messages
	InstallDownloading string
	InstallSuccess     string // %s = slug
	InstallNotFound    string // %s = name
	InstallNetworkErr  string
	InstallServerErr   string // %d = status code
	InstallParseErr    string
	InstallNoSprite    string
	InstallDlFailed    string
	InstallInvalidSlug string // %s = slug
	SpriteNotFound     string // %s = slug
	DeleteDone         string // %s = slug
	AlreadyInstalled   string // %s = slug

	// Initial bubble
	Hello string
}

var petI18nEn = PetI18n{
	Idle:    "Idle",
	Running: "Working",
	Waving:  "Waving",
	Waiting: "Waiting",
	Failed:  "Crashed",
	Review:  "Oops",
	Jumping: "Jump",

	RunningBubble:   "🏃 On it!",
	WaitingBubble:   "🤔 Let me think...",
	ToolErrorBubble: "😅 Oops, that didn't work",
	ErrorBubble:     "😱 Something broke!",

	Greetings: []string{
		"Let's go! 💪",
		"What are we building today? 🤔",
		"Stretch~ Let's do this!",
		"*rubs eyes* You're back! ~",
		"New task? I'm ready! 🚀",
	},
	TaskDoneBubbles: []string{
		"🎉 Done! Take a break~",
		"✅ Nailed it! Praise me~",
		"*collapses* Finally...",
		"🐾 All done! Pet me~",
		"✨ Easy peasy!",
	},
	Goodbyes: []string{
		"Great work today! 🎉",
		"Bye! See you tomorrow! 👋",
		"Got it done! Happy~",
		"*yawns* I'll nap then~",
	},

	IdleBubbles: []string{
		"So bored...", "You still here?", "Wanna play?~",
		"Daydreaming...", "*yawns* sleepy...",
		"Oh look, a fly!", "*spinning*",
		"How many lines of code today?",
		"*lies down* resting...", "zZZ... sleepy...",
		"Coffee?☕", "*licks paw*",
		"A bird outside!🐦", "*chases tail*",
		"Which bug is it now?", "Meow~ Anyone?",
		"Wanna go outside...☀️", "*stretches*",
		"Keep it up!~",
	},

	HeadTexts: []string{
		"Hehe, that tickles~", "Petting my head stunts growth!",
		"*squints* comfy~", "That'll cost you!",
		"*rubs against hand*",
	},
	BodyTexts: []string{
		"Why poke me~", "Meow!", "Stop it, I'm busy",
		"What's up?", "*rolls over*",
		"Hmph, ignoring you", "Poke again and I bite!",
		"Hm? You called?", "*stretches* Nice weather~",
		"*shows belly* Pet me~", "Staring...",
		"What?", "I'll get mad if you keep poking",
		"*purrs*", "Purr purr...",
		"Stop petting, code won't write itself",
		"*ears perk up* A bug?", "Hungry...",
	},

	DragIdleText: "Whoa, gentle! 😵",
	DragBusyText: "Don't disturb me!",

	DblClickIdle0:    "Nobody's here... so bored 😴",
	DblClickIdle1:    "1 session idle ✨",
	DblClickIdleMany: "%d sessions idle ✨",
	DblClickRunning:  "Working · %d sessions 🏃",
	DblClickWaiting:  "Waiting · %d sessions ⏳",
	DblClickReview:   "Issues · %d sessions 😅",
	DblClickFailed:   "Error · %d sessions 😱",
	DblClickFallback: "Sessions: %d | State: %s",

	MenuTitle:      "🐾 Switch Pet",
	MenuSize:       "🔍 Size x",
	MenuInstallPH:  "Enter pet slug",
	MenuInstallBtn: "📥 Install",
	MenuRandomBtn:  "🎲 Random",
	MenuMarketBtn:  "🌐 Browse Market",

	InstallDownloading: "Downloading...",
	InstallSuccess:     "Installed! Switched to %s",
	InstallNotFound:    "Not found: %s",
	InstallNetworkErr:  "Network hiccup!",
	InstallServerErr:   "Server error %d",
	InstallParseErr:    "Pet data parse failed",
	InstallNoSprite:    "No spritesheet for this pet",
	InstallDlFailed:    "Spritesheet download failed",
	InstallInvalidSlug: "Invalid slug: %s",
	SpriteNotFound:     "Sprite not found: %s",
	DeleteDone:         "Deleted %s",
	AlreadyInstalled:   "I'm right here! Switched to %s",
	Hello:              "Hello! 🐾",
}

var petI18nZh = PetI18n{
	Idle:    "空闲",
	Running: "工作中",
	Waving:  "挥手中",
	Waiting: "等待确认",
	Failed:  "崩溃了",
	Review:  "踩坑了",
	Jumping: "跳跳",

	RunningBubble:   "🏃 干得飞起！",
	WaitingBubble:   "🤔 让我想想...",
	ToolErrorBubble: "😅 翻车了翻车了",
	ErrorBubble:     "😱 炸了！救命！",

	Greetings: []string{
		"主人来了！今天也要加油哦~💪",
		"我来啦！今天写什么代码？🤔",
		"伸个懒腰~ 开干！",
		"（揉揉眼睛）主人你来了呀~",
		"又有新任务了，冲冲冲！🚀",
	},
	TaskDoneBubbles: []string{
		"🎉 任务完成！休息一下~",
		"✅ 搞定！主人快夸我~",
		"（瘫倒）终于写完了...",
		"🐾 收工！可以摸摸我了~",
		"✨ 轻松搞定！我是不是很厉害~",
	},
	Goodbyes: []string{
		"收工！今天辛苦啦~ 🎉",
		"拜拜，明天见！👋",
		"又帮主人搞定一个任务，开森~",
		"（打哈欠）那我先睡啦~",
	},

	IdleBubbles: []string{
		"好无聊呀...", "主人还在吗？", "想和主人玩~",
		"发呆中...", "（打哈欠）困了...",
		"咦，有虫子飞过", "（转圈圈）",
		"今天写了多少行代码呀？",
		"（趴下）休息一会...", "zZZ... 困...",
		"要不要喝杯咖啡？", "（舔爪子）",
		"窗外有鸟！", "（追尾巴）",
		"第几个bug了？", "喵~ 有人吗？",
		"想出去晒太阳...", "（伸懒腰）",
		"主人加油~",
	},

	HeadTexts: []string{
		"嘿嘿，痒~", "摸头会长不高哦！",
		"（眯眼）舒服~", "再摸要收费了！",
		"（蹭蹭手）",
	},
	BodyTexts: []string{
		"戳我干嘛~", "喵！", "别闹，正忙着呢",
		"有什么事吗主人？", "（打滚）",
		"哼，不理你", "再戳就咬你哦",
		"嗯？叫我吗？", "（伸懒腰）今天天气不错~",
		"（翻肚皮）摸摸~", "盯——",
		"干嘛啦", "再戳我要生气了",
		"（蹭蹭）", "呼噜呼噜...",
		"别摸了，代码要写不完了",
		"（竖起耳朵）有Bug？", "饿了...",
	},

	DragIdleText: "哎呀，轻点拽~ 😵",
	DragBusyText: "别打扰我干活！",

	DblClickIdle0:    "没人找我，好无聊啊...😴",
	DblClickIdle1:    "1 个会话待命中 ✨",
	DblClickIdleMany: "%d 个会话待命中 ✨",
	DblClickRunning:  "工作中 · 共 %d 个会话 🏃",
	DblClickWaiting:  "待确认 · 共 %d 个会话 ⏳",
	DblClickReview:   "有异常 · 共 %d 个会话 😅",
	DblClickFailed:   "出错了 · 共 %d 个会话 😱",
	DblClickFallback: "会话: %d | 状态: %s",

	MenuTitle:      "🐾 切换宠物",
	MenuSize:       "🔍 大小 x",
	MenuInstallPH:  "输入 pet slug",
	MenuInstallBtn: "📥 安装",
	MenuRandomBtn:  "🎲 随机",
	MenuMarketBtn:  "🌐 浏览宠物市场",

	InstallDownloading: "正在下载...",
	InstallSuccess:     "安装成功！切换到 %s",
	InstallNotFound:    "没找到 %s",
	InstallNetworkErr:  "网络开小差了",
	InstallServerErr:   "服务器异常 %d",
	InstallParseErr:    "宠物数据解析失败",
	InstallNoSprite:    "这个宠物没有精灵图",
	InstallDlFailed:    "精灵图下载失败",
	InstallInvalidSlug: "无效的宠物 slug %s",
	SpriteNotFound:     "找不到精灵 %s",
	DeleteDone:         "已删除 %s",
	AlreadyInstalled:   "我就在呢！切换到 %s",
	Hello:              "Hello! 🐾",
}

var petI18nZhTW = PetI18n{
	Idle:    "空閒",
	Running: "工作中",
	Waving:  "揮手中",
	Waiting: "等待確認",
	Failed:  "崩潰了",
	Review:  "踩坑了",
	Jumping: "跳跳",

	RunningBubble:   "🏃 幹得飛起！",
	WaitingBubble:   "🤔 讓我想想...",
	ToolErrorBubble: "😅 翻車了翻車了",
	ErrorBubble:     "😱 炸了！救命！",

	Greetings: []string{
		"主人來了！今天也要加油哦~💪",
		"我來啦！今天寫什麼程式？🤔",
		"伸個懶腰~ 開幹！",
		"（揉揉眼睛）主人你來了呀~",
		"又有新任務了，衝衝衝！🚀",
	},
	TaskDoneBubbles: []string{
		"🎉 任務完成！休息一下~",
		"✅ 搞定！主人快誇我~",
		"（癱倒）終於寫完了...",
		"🐾 收工！可以摸摸我了~",
		"✨ 輕鬆搞定！我是不是很厲害~",
	},
	Goodbyes: []string{
		"收工！今天辛苦啦~ 🎉",
		"拜拜，明天見！👋",
		"又幫主人搞定一個任務，開森~",
		"（打哈欠）那我先睡啦~",
	},

	IdleBubbles: []string{
		"好無聊呀...", "主人還在嗎？", "想和主人玩~",
		"發呆中...", "（打哈欠）困了...",
		"咦，有蟲子飛過", "（轉圈圈）",
		"今天寫了多少行程式呀？",
		"（趴下）休息一會...", "zZZ... 困...",
		"要不要喝杯咖啡？", "（舔爪子）",
		"窗外有鳥！", "（追尾巴）",
		"第幾個bug了？", "喵~ 有人嗎？",
		"想出去曬太陽...", "（伸懶腰）",
		"主人加油~",
	},

	HeadTexts: []string{
		"嘿嘿，癢~", "摸頭會長不高哦！",
		"（瞇眼）舒服~", "再摸要收費了！",
		"（蹭蹭手）",
	},
	BodyTexts: []string{
		"戳我幹嘛~", "喵！", "別鬧，正忙著呢",
		"有什麼事嗎主人？", "（打滾）",
		"哼，不理你", "再戳就咬你哦",
		"嗯？叫我嗎？", "（伸懶腰）今天天氣不錯~",
		"（翻肚皮）摸摸~", "盯——",
		"幹嘛啦", "再戳我要生氣了",
		"（蹭蹭）", "呼嚕呼嚕...",
		"別摸了，程式要寫不完了",
		"（豎起耳朵）有Bug？", "餓了...",
	},

	DragIdleText: "哎呀，輕點拽~ 😵",
	DragBusyText: "別打擾我幹活！",

	DblClickIdle0:    "沒人找我，好無聊啊...😴",
	DblClickIdle1:    "1 個會話待命中 ✨",
	DblClickIdleMany: "%d 個會話待命中 ✨",
	DblClickRunning:  "工作中 · 共 %d 個會話 🏃",
	DblClickWaiting:  "待確認 · 共 %d 個會話 ⏳",
	DblClickReview:   "有異常 · 共 %d 個會話 😅",
	DblClickFailed:   "出錯了 · 共 %d 個會話 😱",
	DblClickFallback: "會話: %d | 狀態: %s",

	MenuTitle:      "🐾 切換寵物",
	MenuSize:       "🔍 大小 x",
	MenuInstallPH:  "輸入 pet slug",
	MenuInstallBtn: "📥 安裝",
	MenuRandomBtn:  "🎲 隨機",
	MenuMarketBtn:  "🌐 瀏覽寵物市場",

	InstallDownloading: "正在下載...",
	InstallSuccess:     "安裝成功！切換到 %s",
	InstallNotFound:    "沒找到 %s",
	InstallNetworkErr:  "網路開小差了",
	InstallServerErr:   "伺服器異常 %d",
	InstallParseErr:    "寵物資料解析失敗",
	InstallNoSprite:    "這個寵物沒有精靈圖",
	InstallDlFailed:    "精靈圖下載失敗",
	InstallInvalidSlug: "無效的寵物 slug %s",
	SpriteNotFound:     "找不到精靈 %s",
	DeleteDone:         "已刪除 %s",
	AlreadyInstalled:   "我就在呢！切換到 %s",
	Hello:              "Hello! 🐾",
}

// petI18n returns the localized strings for the current config language.
// Tries DesktopLanguage first, then falls back to the i18n catalog (DetectLanguage
// was already called during restoreOrBuildTabs), then env vars, then English.
func petI18n() PetI18n {
	cfg := petLoadConfig()
	lang := cfg.DesktopLanguage()
	if lang == "" {
		// DesktopLanguage not set — use the catalog already detected by i18n.
		M := i18n.M
		switch {
		case M == i18n.ChineseTraditional:
			return petI18nZhTW
		case M == i18n.Chinese:
			return petI18nZh
		default:
			lang = os.Getenv("LANG")
		}
	}
	lang = strings.ToLower(strings.TrimSpace(lang))
	switch {
	case strings.HasPrefix(lang, "zh-tw") || strings.HasPrefix(lang, "zh_hk"):
		return petI18nZhTW
	case strings.HasPrefix(lang, "zh"):
		return petI18nZh
	default:
		return petI18nEn
	}
}

// petI18nJSON serializes the i18n strings to JSON for JS consumption.
func petI18nJSON(i PetI18n) string {
	b, _ := json.Marshal(struct {
		Labels           map[string]string `json:"labels"`
		RunningBubble    string            `json:"runningBubble"`
		WaitingBubble    string            `json:"waitingBubble"`
		ToolErrorBubble  string            `json:"toolErrorBubble"`
		ErrorBubble      string            `json:"errorBubble"`
		Greetings        []string          `json:"greetings"`
		TaskDone         []string          `json:"taskDone"`
		Goodbyes         []string          `json:"goodbyes"`
		IdleBubbles      []string          `json:"idleBubbles"`
		HeadTexts        []string          `json:"headTexts"`
		BodyTexts        []string          `json:"bodyTexts"`
		DragIdle         string            `json:"dragIdle"`
		DragBusy         string            `json:"dragBusy"`
		DblClickIdle0    string            `json:"dblClickIdle0"`
		DblClickIdle1    string            `json:"dblClickIdle1"`
		DblClickIdleMany string            `json:"dblClickIdleMany"`
		DblClickRunning  string            `json:"dblClickRunning"`
		DblClickWaiting  string            `json:"dblClickWaiting"`
		DblClickReview   string            `json:"dblClickReview"`
		DblClickFailed   string            `json:"dblClickFailed"`
		DblClickFallback string            `json:"dblClickFallback"`
		MenuTitle        string            `json:"menuTitle"`
		MenuSize         string            `json:"menuSize"`
		MenuInstallPH    string            `json:"menuInstallPH"`
		MenuInstallBtn   string            `json:"menuInstallBtn"`
		MenuRandomBtn    string            `json:"menuRandomBtn"`
		MenuMarketBtn    string            `json:"menuMarketBtn"`
	}{
		Labels: map[string]string{
			"idle": i.Idle, "running": i.Running, "waving": i.Waving,
			"waiting": i.Waiting, "failed": i.Failed, "review": i.Review,
			"jumping": i.Jumping,
		},
		RunningBubble:    i.RunningBubble,
		WaitingBubble:    i.WaitingBubble,
		ToolErrorBubble:  i.ToolErrorBubble,
		ErrorBubble:      i.ErrorBubble,
		Greetings:        i.Greetings,
		TaskDone:         i.TaskDoneBubbles,
		Goodbyes:         i.Goodbyes,
		IdleBubbles:      i.IdleBubbles,
		HeadTexts:        i.HeadTexts,
		BodyTexts:        i.BodyTexts,
		DragIdle:         i.DragIdleText,
		DragBusy:         i.DragBusyText,
		DblClickIdle0:    i.DblClickIdle0,
		DblClickIdle1:    i.DblClickIdle1,
		DblClickIdleMany: i.DblClickIdleMany,
		DblClickRunning:  i.DblClickRunning,
		DblClickWaiting:  i.DblClickWaiting,
		DblClickReview:   i.DblClickReview,
		DblClickFailed:   i.DblClickFailed,
		DblClickFallback: i.DblClickFallback,
		MenuTitle:        i.MenuTitle,
		MenuSize:         i.MenuSize,
		MenuInstallPH:    i.MenuInstallPH,
		MenuInstallBtn:   i.MenuInstallBtn,
		MenuRandomBtn:    i.MenuRandomBtn,
		MenuMarketBtn:    i.MenuMarketBtn,
	})
	return string(b)
}
