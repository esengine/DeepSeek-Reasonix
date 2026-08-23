package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/billing"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

func TestTurnReceiptKeepsCompletePerTurnBreakdown(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	defer i18n.DetectLanguage("en")
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")
	i18n.DetectLanguage("zh")

	u := &provider.Usage{
		PromptTokens:     13_625,
		CompletionTokens: 392,
		TotalTokens:      14_017,
		CacheHitTokens:   13_184,
		CacheMissTokens:  441,
		ReasoningTokens:  24,
	}
	p := &provider.Pricing{CacheHit: .1, Input: 1, Output: 2}
	got := renderTurnReceipt(u, p, nil)
	for _, want := range []string{
		"本轮", "14.0K tok", "in 13.6K", "cached 13.2K", "new 441",
		"out 392", "reasoning 24", "¥0.0025",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("turn receipt %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "\033[") {
		t.Fatalf("NO_COLOR turn receipt contains escapes: %q", got)
	}
}

func TestTurnReceiptShowsOccurrenceRateBand(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	defer i18n.DetectLanguage("en")
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")
	i18n.DetectLanguage("zh")
	u := &provider.Usage{PromptTokens: 1, TotalTokens: 1}
	q := &billing.CostQuote{Original: billing.Money{Amount: "0.01", Currency: "CNY"}, CostComplete: true, RateBand: billing.RateBandOffPeak}
	if got := ansi.Strip(renderQuotedTurnReceipt(u, q, nil)); !strings.Contains(got, "低峰") {
		t.Fatalf("receipt = %q, want low-rate label", got)
	}
	q.RateBand = ""
	if got := ansi.Strip(renderQuotedTurnReceipt(u, q, nil)); strings.Contains(got, "低峰") || strings.Contains(got, "高峰") {
		t.Fatalf("legacy receipt invented a rate band: %q", got)
	}
}

func TestSessionCostStatusAggregatesRateBandsWithoutRepricing(t *testing.T) {
	defer i18n.DetectLanguage("en")
	i18n.DetectLanguage("zh")
	quote := func(amount, band string) *billing.CostQuote {
		selected := billing.Money{Amount: amount, Currency: "CNY"}
		return &billing.CostQuote{
			Original: selected, Selected: &selected, Valuations: map[string]billing.Valuation{
				"CNY": {Money: selected, Basis: billing.BasisIdentity},
			},
			CostComplete: true, DisplayComplete: true, Complete: true, RateBand: band,
		}
	}
	var m chatTUI
	m.addSessionCostQuote(quote("1", billing.RateBandPeak))
	m.addSessionCostQuote(quote("2", billing.RateBandOffPeak))
	if got := ansi.Strip(m.sessionCostStatus()); !strings.Contains(got, "¥3.0000") || !strings.Contains(got, "混合档位") {
		t.Fatalf("session cost = %q", got)
	}
	m.addSessionCostQuote(quote("1", ""))
	if got := ansi.Strip(m.sessionCostStatus()); strings.Contains(got, "混合档位") || strings.Contains(got, "高峰") || strings.Contains(got, "低峰") {
		t.Fatalf("unknown member retained a claimed band: %q", got)
	}
}

func TestTurnReceiptFallsBackToDerivedFreshTokensAndWrapsCleanly(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	defer i18n.DetectLanguage("en")
	activeColorProfile = colorprofile.ANSI256
	configureCLITheme("dark")
	i18n.DetectLanguage("en")

	got := renderTurnReceipt(&provider.Usage{
		PromptTokens: 1_200, CompletionTokens: 80, TotalTokens: 1_280, CacheHitTokens: 900,
	}, nil, &event.CacheDiagnostics{PrefixChanged: true, PrefixChangeReasons: []string{"tools"}})
	plain := ansi.Strip(got)
	for _, want := range []string{"TURN", "cached 900", "new 300", "cache prefix changed: tools"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("turn receipt %q missing %q", plain, want)
		}
	}
	for i, line := range strings.Split(wrapTranscript(got, 32), "\n") {
		if width := visibleWidth(line); width > 32 {
			t.Fatalf("wrapped turn receipt row %d width = %d, want <= 32: %q", i, width, line)
		}
	}
}

func TestTurnReceiptIgnoresEmptyUsage(t *testing.T) {
	if got := renderTurnReceipt(nil, nil, nil); got != "" {
		t.Fatalf("nil usage receipt = %q, want empty", got)
	}
	if got := renderTurnReceipt(&provider.Usage{}, nil, nil); got != "" {
		t.Fatalf("empty usage receipt = %q, want empty", got)
	}
}

func TestTurnReceiptMarksEstimatedUsage(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	defer i18n.DetectLanguage("en")
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")
	i18n.DetectLanguage("en")

	got := renderTurnReceipt(&provider.Usage{TotalTokens: 1_024, Estimated: true}, nil, nil)
	for _, want := range []string{"≈1.0K tok", "estimated"} {
		if !strings.Contains(got, want) {
			t.Fatalf("estimated turn receipt %q missing %q", got, want)
		}
	}
}

func TestTurnReceiptBandUsesSingleQuietBoundary(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	band := renderTurnReceiptBand("  TURN  14.0K tok · in 13.6K", 48)
	lines := strings.Split(band, "\n")
	if len(lines) != 2 {
		t.Fatalf("turn receipt band rows = %d, want top rule and receipt:\n%s", len(lines), band)
	}
	if strings.Trim(lines[0], "─ ") != "" {
		t.Fatalf("turn receipt band boundary is not a rule:\n%s", band)
	}
	if got := visibleWidth(lines[0]); got != 48 {
		t.Fatalf("receipt rule width = %d, want 48: %q", got, lines[0])
	}
	if !strings.Contains(lines[1], "TURN  14.0K tok") {
		t.Fatalf("receipt body missing from quiet band:\n%s", band)
	}
}

func TestTurnReceiptAdaptsContrastAcrossThemes(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	defer i18n.DetectLanguage("en")
	activeColorProfile = colorprofile.ANSI256
	i18n.DetectLanguage("en")

	for _, tt := range []struct {
		mode, borderSGR, labelSGR, valueSGR string
	}{
		{mode: "dark", borderSGR: "\033[38;5;237m", labelSGR: "\033[38;5;248m", valueSGR: "\033[38;5;251m"},
		{mode: "light", borderSGR: "\033[38;5;252m", labelSGR: "\033[38;5;241m", valueSGR: "\033[38;5;239m"},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			configureCLITheme(tt.mode)
			receipt := renderTurnReceipt(&provider.Usage{
				PromptTokens: 900, CompletionTokens: 100, TotalTokens: 1_000,
			}, nil, nil)
			band := renderTurnReceiptBand(receipt, 80)
			for _, want := range []string{tt.borderSGR + "─", tt.labelSGR + "TURN", tt.valueSGR + "1.0K tok"} {
				if !strings.Contains(band, want) {
					t.Fatalf("%s receipt %q missing semantic style %q", tt.mode, band, want)
				}
			}
			if strings.Count(ansi.Strip(band), "\n") != 1 {
				t.Fatalf("%s receipt should keep one rule and one body row: %q", tt.mode, ansi.Strip(band))
			}
		})
	}
}

func TestStatusFooterSemanticPaletteAcrossThemes(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	for _, tt := range []struct {
		mode, labelSGR, valueSGR, infoSGR string
	}{
		{mode: "dark", labelSGR: "\033[38;5;248m", valueSGR: "\033[38;5;251m", infoSGR: "\033[38;5;80m"},
		{mode: "light", labelSGR: "\033[38;5;241m", valueSGR: "\033[38;5;239m", infoSGR: "\033[38;5;25m"},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			configureCLITheme(tt.mode)
			m := newTestChatTUI()
			m.label = "deepseek-v4-flash"
			m.effortLevel = "auto"
			got := m.statusModelWorkGroup(80)
			for _, want := range []string{
				tt.labelSGR + "MODEL",
				tt.infoSGR + "deepseek-v4-flash",
				tt.labelSGR + "EFFORT",
				tt.valueSGR + "auto",
			} {
				if !strings.Contains(got, want) {
					t.Fatalf("model/work group %q missing semantic style %q", got, want)
				}
			}
			primary := m.primaryStatusLine(" Auto ", false, false)
			if !strings.Contains(primary, tt.valueSGR+i18n.M.ChatStatusIdle) ||
				!strings.Contains(primary, tt.labelSGR+i18n.M.ChatStatusCycleHintCompact) {
				t.Fatalf("%s interaction hints should use readable semantic contrast: %q", tt.mode, primary)
			}
		})
	}
}

func TestStatusFooterThemesKeepIdenticalGeometry(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.label = "deepseek-v4-flash"
	m.effortLevel = "max"
	m.balance = "¥12.34"
	m.gitStatus = gitStatus{Repo: "DeepSeek-Reasonix", Branch: "feature/theme-footer", Added: 3}

	render := func(mode string, profile colorprofile.Profile) string {
		activeColorProfile = profile
		configureCLITheme(mode)
		primary := m.primaryStatusLine(" Auto ", false, false)
		return ansi.Strip(m.renderStatusBlock(primary, 132))
	}
	dark := render("dark", colorprofile.ANSI256)
	light := render("light", colorprofile.ANSI256)
	plain := render("dark", colorprofile.NoTTY)
	if dark != light || dark != plain {
		t.Fatalf("theme modes changed footer geometry:\ndark:\n%s\nlight:\n%s\nplain:\n%s", dark, light, plain)
	}
}

func TestStatusFooterGitAndDividerAdaptToTheme(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	for _, tt := range []struct {
		mode, gitSGR, borderSGR string
	}{
		{mode: "dark", gitSGR: "\033[38;5;179m", borderSGR: "\033[38;5;237m"},
		{mode: "light", gitSGR: "\033[38;5;136m", borderSGR: "\033[38;5;252m"},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			configureCLITheme(tt.mode)
			m := newTestChatTUI()
			m.gitStatus = gitStatus{Repo: "DeepSeek-Reasonix", Branch: "db4be5e6", Detached: true}
			git := m.layoutGitTelemetry(80)
			if !strings.Contains(git, tt.gitSGR+"DeepSeek-Reasonix") {
				t.Fatalf("%s Git identity should use warm semantic colour: %q", tt.mode, git)
			}
			divider := statusFooterDivider(40)
			if !strings.Contains(divider, tt.borderSGR) || visibleWidth(divider) != 40 {
				t.Fatalf("%s divider should use border token at full width: %q", tt.mode, divider)
			}
		})
	}
}

func TestContextFooterColorsOnlyValuesByUrgency(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256
	configureCLITheme("dark")

	normal := strings.Join(renderContextStatusGroups(10, 100, .8), " ")
	if !strings.Contains(normal, "\033[38;5;248mCTX") || !strings.Contains(normal, "\033[38;5;251m10 (10%)") {
		t.Fatalf("normal context should use subtle label and neutral value: %q", normal)
	}

	warning := strings.Join(renderContextStatusGroups(75, 100, .8), " ")
	if !strings.Contains(warning, "\033[38;5;248mCOMPACT") || !strings.Contains(warning, "\033[38;5;179m5%") {
		t.Fatalf("near-threshold context should warn only on values: %q", warning)
	}

	critical := strings.Join(renderContextStatusGroups(80, 100, .8), " ")
	if !strings.Contains(critical, "\033[38;5;179m80 (80%)") || !strings.Contains(critical, "\033[38;5;167m0%") {
		t.Fatalf("critical context should keep warning/danger hierarchy: %q", critical)
	}
}

func TestStatusFooterNoColorKeepsSemanticLabels(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	m := newTestChatTUI()
	m.label = "deepseek-v4-flash"
	m.effortLevel = "auto"
	m.balance = "¥12.34"
	block := m.renderStatusBlock(m.primaryStatusLine(" Auto ", false, false), 120)
	if strings.Contains(block, "\033[") {
		t.Fatalf("NO_COLOR footer contains escapes: %q", block)
	}
	for _, want := range []string{"MODEL deepseek-v4-flash", "EFFORT auto", "BAL ¥12.34"} {
		if !strings.Contains(block, want) {
			t.Fatalf("NO_COLOR footer missing %q:\n%s", want, block)
		}
	}
}

func TestStatusFooterUsesReadableLocalizedHintAndWrapsCleanly(t *testing.T) {
	defer i18n.DetectLanguage("en")
	for _, tt := range []struct {
		lang, compact, session string
	}{
		{lang: "en", compact: "Shift+Tab ask/auto/plan · Ctrl+Y YOLO", session: "MODEL deepseek-v4-flash   EFFORT auto"},
		{lang: "zh", compact: "Shift+Tab 询问/自动/计划 · Ctrl+Y YOLO", session: "模型 deepseek-v4-flash   强度 auto"},
		{lang: "zh-TW", compact: "Shift+Tab 詢問/自動/計畫 · Ctrl+Y YOLO", session: "模型 deepseek-v4-flash   強度 auto"},
	} {
		t.Run(tt.lang, func(t *testing.T) {
			i18n.DetectLanguage(tt.lang)
			m := newTestChatTUI()
			m.ctrl = control.New(control.Options{})
			m.label = "deepseek-v4-flash"
			m.effortLevel = "auto"

			primary := m.primaryStatusLine(" Auto ", false, false)
			block := ansi.Strip(m.renderStatusBlock(primary, 80))
			lines := strings.Split(block, "\n")
			if len(lines) != 2 {
				t.Fatalf("localized footer rows = %d, want wrapped primary/session rows without an empty data band:\n%s", len(lines), block)
			}
			if !strings.Contains(lines[0], tt.compact) || !strings.Contains(lines[1], tt.session) {
				t.Fatalf("localized footer did not keep readable shortcut and session groups:\n%s", block)
			}
			if strings.Contains(block, "WORK") || strings.Contains(block, "模式") {
				t.Fatalf("localized footer should not show execution-mode UI:\n%s", block)
			}
			if strings.Contains(block, "⇧Tab") || strings.Contains(block, "^Y") {
				t.Fatalf("localized footer fell back to symbolic shortcut notation:\n%s", block)
			}
			for row, line := range lines {
				if width := visibleWidth(line); width > 80 {
					t.Fatalf("localized footer row %d width = %d, want <= 80: %q", row, width, line)
				}
			}

			narrow := ansi.Strip(m.renderStatusBlock(primary, 24))
			if strings.Contains(narrow, "Shift+Tab") || strings.Contains(narrow, "Ctrl+Y") {
				t.Fatalf("shortcut help should yield when readable key names cannot fit:\n%s", narrow)
			}
			if !strings.Contains(narrow, ansi.Strip(footerValue(i18n.M.ChatStatusIdle))) {
				t.Fatalf("narrow footer should preserve the idle state:\n%s", narrow)
			}
		})
	}
}

func TestStatusFooterLocalizesMetricLabelsAndKeepsNarrowRows(t *testing.T) {
	defer i18n.DetectLanguage("en")
	for _, tt := range []struct {
		lang      string
		session   string
		telemetry []string
	}{
		{
			lang:      "zh",
			session:   "模型 deepseek-v4-flash   强度 auto",
			telemetry: []string{"缓存", "上下文", "压缩", "任务", "余额"},
		},
		{
			lang:      "zh-TW",
			session:   "模型 deepseek-v4-flash   強度 auto",
			telemetry: []string{"快取", "上下文", "壓縮", "任務", "餘額"},
		},
	} {
		t.Run(tt.lang, func(t *testing.T) {
			i18n.DetectLanguage(tt.lang)
			m := newTestChatTUI()
			m.label = "deepseek-v4-flash"
			m.effortLevel = "auto"
			if got := ansi.Strip(m.statusModelWorkGroup(80)); got != tt.session {
				t.Fatalf("localized session metrics = %q, want %q", got, tt.session)
			}

			groups := []string{
				footerMetric(i18n.M.ChatStatusCacheLabel, footerValue("90%")),
			}
			groups = append(groups, renderContextStatusGroups(75, 100, .8)...)
			groups = append(groups,
				footerMetric(i18n.M.ChatStatusJobsLabel, footerInfo("2")),
				footerMetric(i18n.M.ChatStatusBalanceLabel, footerValue("¥12.34")),
			)
			packed := ansi.Strip(packStatusGroups(groups, 22))
			for _, label := range tt.telemetry {
				if !strings.Contains(packed, label+" ") {
					t.Fatalf("localized telemetry missing %q:\n%s", label, packed)
				}
			}
			for row, line := range strings.Split(packed, "\n") {
				if width := visibleWidth(line); width > 22 {
					t.Fatalf("localized telemetry row %d width = %d, want <= 22: %q", row, width, line)
				}
			}
		})
	}
}

func TestStatusFooterSwapsModelAndGitGroups(t *testing.T) {
	i18n.DetectLanguage("en")

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.label = "deepseek-v4-flash"
	m.effortLevel = "auto"
	m.balance = "¥12.34"
	m.gitStatus = gitStatus{
		Repo:      "DeepSeek-Reasonix",
		Branch:    "feature/responsive-footer",
		Added:     1199,
		Removed:   244,
		Untracked: 3,
	}

	primary := m.primaryStatusLine(" Auto ", false, false)
	lines := strings.Split(ansi.Strip(m.renderStatusBlock(primary, 160)), "\n")
	if len(lines) != 3 {
		t.Fatalf("wide status block lines = %d, want two data rows plus divider:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[0], "MODEL deepseek-v4-flash   EFFORT auto") {
		t.Fatalf("first row should keep model and effort in one session group:\n%s", strings.Join(lines, "\n"))
	}
	if strings.Contains(lines[0], "DeepSeek-Reasonix@") {
		t.Fatalf("first row should not contain Git identity:\n%s", strings.Join(lines, "\n"))
	}
	if strings.Trim(lines[1], "─ ") != "" {
		t.Fatalf("middle row should be a divider:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[2], "DeepSeek-Reasonix@feature/responsive-footer") || strings.Contains(lines[2], "…") {
		t.Fatalf("second row should preserve the full Git identity when it fits:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[2], "+1199 -244 ?3") || !strings.HasSuffix(lines[2], "BAL ¥12.34") {
		t.Fatalf("second row should preserve Git changes and right-anchor telemetry:\n%s", strings.Join(lines, "\n"))
	}
	for i, line := range lines {
		if got := visibleWidth(line); got > 160 {
			t.Fatalf("row %d width = %d, want <= 160: %q", i, got, line)
		}
	}
}

func TestStatusFooterWithoutGitLeftAlignsTelemetry(t *testing.T) {
	defer i18n.DetectLanguage("en")
	i18n.DetectLanguage("en")

	m := newTestChatTUI()
	m.balance = "¥12.34"
	line := ansi.Strip(m.layoutGitTelemetry(120))
	if !strings.HasPrefix(line, statusFooterIndent+"BAL ¥12.34") {
		t.Fatalf("non-Git telemetry should be left aligned, got %q", line)
	}
	if visibleWidth(line) >= 120 {
		t.Fatalf("non-Git telemetry unexpectedly retained right-alignment padding: %q", line)
	}
}

func TestStatusFooterOmitsEmptyDataBand(t *testing.T) {
	m := newTestChatTUI()
	primary := "  Auto · ready"
	block := ansi.Strip(m.renderStatusBlock(primary, 120))
	if block != primary {
		t.Fatalf("empty Git/telemetry status block = %q, want only %q", block, primary)
	}
	if strings.Contains(block, "─") {
		t.Fatalf("empty Git/telemetry status block retained a divider: %q", block)
	}
}

func TestStatusFooterMediumLayoutLeftAlignsModelWork(t *testing.T) {
	i18n.DetectLanguage("en")

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.label = "deepseek-v4-flash"
	m.effortLevel = "auto"

	primary := m.primaryStatusLine(" Auto ", false, false)
	lines := strings.Split(ansi.Strip(m.renderStatusBlock(primary, 82)), "\n")
	if len(lines) != 2 {
		t.Fatalf("medium footer rows = %d, want primary plus model/effort without an empty data band:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	modelRow := lines[1]
	if !strings.HasPrefix(modelRow, statusFooterIndent+"MODEL deepseek-v4-flash") ||
		!strings.Contains(modelRow, "EFFORT auto") {
		t.Fatalf("medium model/effort row should be left aligned, got %q:\n%s", modelRow, strings.Join(lines, "\n"))
	}
	if strings.Count(strings.TrimLeft(modelRow, " "), "MODEL") != 1 {
		t.Fatalf("medium model/work row should remain a single semantic group: %q", modelRow)
	}
}

func TestStatusFooterStacksGitAndTelemetryWithoutFloatingContinuation(t *testing.T) {
	i18n.DetectLanguage("en")

	m := newTestChatTUI()
	m.gitStatus = gitStatus{
		Repo: "DeepSeek-Reasonix", Branch: "feature/responsive-footer", Added: 20, Removed: 4,
	}
	m.balance = "¥123.45"

	lines := strings.Split(ansi.Strip(m.layoutGitTelemetry(56)), "\n")
	if len(lines) != 2 {
		t.Fatalf("stacked Git/telemetry rows = %d, want 2:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.HasPrefix(lines[0], statusFooterIndent+"DeepSeek-Reasonix@") || !strings.Contains(lines[0], "+20 -4") {
		t.Fatalf("Git should own the complete first row:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.HasPrefix(lines[1], statusFooterIndent+"BAL ¥123.45") {
		t.Fatalf("stacked telemetry should be left aligned, got %q", lines[1])
	}
}

func TestStatusFooterNarrowLayoutBreaksBetweenGroups(t *testing.T) {
	i18n.DetectLanguage("en")

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.label = "provider/" + strings.Repeat("long-model-", 8)
	m.balance = "¥123.45"
	m.gitStatus = gitStatus{
		Repo:    "DeepSeek-Reasonix-Workspace",
		Branch:  "feature/" + strings.Repeat("long-branch-", 8),
		Added:   20,
		Removed: 4,
	}

	primary := m.primaryStatusLine(" Auto ", false, false)
	block := ansi.Strip(m.renderStatusBlock(primary, 40))
	lines := strings.Split(block, "\n")
	if len(lines) <= 2 {
		t.Fatalf("narrow status block lines = %d, want semantic wrapping:\n%s", len(lines), block)
	}
	for i, line := range lines {
		if got := visibleWidth(line); got > 40 {
			t.Fatalf("row %d width = %d, want <= 40: %q", i, got, line)
		}
	}
	if !strings.Contains(block, "@") || !strings.Contains(block, "+20 -4") || !strings.Contains(block, "¥123.45") {
		t.Fatalf("narrow layout dropped required information:\n%s", block)
	}
}

func TestStatusFooterCustomLineStillReplacesBuiltInData(t *testing.T) {
	i18n.DetectLanguage("en")

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.label = "deepseek-v4-flash"
	m.balance = "¥12.34"
	m.statuslineCmd = "custom-status"
	m.statuslineOut = "custom telemetry"
	m.gitStatus = gitStatus{Repo: "Reasonix", Branch: "main"}

	primary := m.primaryStatusLine(" Auto ", false, false)
	block := ansi.Strip(m.renderStatusBlock(primary, 120))
	if strings.Contains(block, "deepseek-v4-flash") || strings.Contains(block, "¥12.34") {
		t.Fatalf("custom statusline should replace built-in data fields:\n%s", block)
	}
	if !strings.Contains(block, "Reasonix@main") || !strings.Contains(block, "custom telemetry") {
		t.Fatalf("custom statusline should coexist with Git identity:\n%s", block)
	}
}

func TestStatusFooterHeightCountUsesRenderedLayout(t *testing.T) {
	i18n.DetectLanguage("en")

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.width = 34
	m.label = "provider/" + strings.Repeat("long-model-", 6)
	m.gitStatus = gitStatus{Repo: "VeryLongWorkspaceName", Branch: strings.Repeat("branch/", 8)}
	m.balance = "¥12.34"

	modeTag := " " + m.modeTagText() + " "
	primary := m.primaryStatusLine(modeTag, false, false)
	want := strings.Count(m.renderStatusBlock(primary, m.width), "\n") + 1
	if got := m.computeStatusLineCount(m.width); got != want {
		t.Fatalf("computed status rows = %d, rendered rows = %d", got, want)
	}
}
