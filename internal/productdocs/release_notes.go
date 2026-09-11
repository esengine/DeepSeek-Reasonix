package productdocs

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type localizedText struct {
	English string `json:"en"`
	Chinese string `json:"zh"`
}

type releaseItem struct {
	Kind    string        `json:"kind"`
	Level   string        `json:"level"`
	Title   localizedText `json:"title"`
	Body    localizedText `json:"body"`
	Href    string        `json:"href"`
	Refs    []int         `json:"refs"`
	Targets []string      `json:"targets"`
}

type releaseRecord struct {
	Version          string        `json:"version"`
	TargetingVersion int           `json:"targetingVersion"`
	Date             string        `json:"date"`
	Channel          string        `json:"channel"`
	Status           string        `json:"status"`
	Title            localizedText `json:"title"`
	Summary          localizedText `json:"summary"`
	Surfaces         []string      `json:"surfaces"`
	Guides           []releaseItem `json:"guides"`
	Highlights       []releaseItem `json:"highlights"`
	Upgrade          []releaseItem `json:"upgrade"`
	Risks            []releaseItem `json:"risks"`
	Contributors     []string      `json:"contributors"`
	Changes          struct {
		New      []releaseItem `json:"new"`
		Improved []releaseItem `json:"improved"`
		Fixed    []releaseItem `json:"fixed"`
	} `json:"changes"`
}

type renderedReleaseDocument struct {
	path    string
	source  string
	locale  string
	version string
	content string
}

func renderReleaseDocuments(data []byte) ([]renderedReleaseDocument, int, error) {
	var catalog struct {
		Releases []releaseRecord `json:"releases"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, 0, fmt.Errorf("decode release-notes/releases.json: %w", err)
	}
	if len(catalog.Releases) == 0 {
		return nil, 0, fmt.Errorf("release-notes/releases.json contains no releases")
	}
	seen := make(map[string]struct{}, len(catalog.Releases))
	documents := make([]renderedReleaseDocument, 0, len(catalog.Releases)*2)
	for _, release := range catalog.Releases {
		if !validReleaseVersion(release.Version) {
			return nil, 0, fmt.Errorf("release-notes/releases.json has invalid version %q", release.Version)
		}
		if _, duplicate := seen[release.Version]; duplicate {
			return nil, 0, fmt.Errorf("release-notes/releases.json has duplicate version %q", release.Version)
		}
		seen[release.Version] = struct{}{}
		for _, locale := range []string{"en", "zh-CN"} {
			suffix := ".md"
			if locale == "zh-CN" {
				suffix = ".zh-CN.md"
			}
			documents = append(documents, renderedReleaseDocument{
				path:    "changelog/v" + release.Version + suffix,
				source:  "release-notes/releases.json#v" + release.Version,
				locale:  locale,
				version: release.Version,
				content: renderReleaseMarkdown(release, locale),
			})
		}
	}
	return documents, len(catalog.Releases), nil
}

func validReleaseVersion(version string) bool {
	if version == "" || strings.Contains(version, "..") {
		return false
	}
	for _, r := range version {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func renderReleaseMarkdown(release releaseRecord, locale string) string {
	zh := locale == "zh-CN"
	localize := func(value localizedText) string {
		if zh {
			return strings.TrimSpace(value.Chinese)
		}
		return strings.TrimSpace(value.English)
	}
	heading := func(en, cn string) string {
		if zh {
			return cn
		}
		return en
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Reasonix v%s — %s\n\n", release.Version, localize(release.Title))
	if zh {
		fmt.Fprintf(&b, "发布日期：%s\n\n发布渠道：%s\n\n状态：%s\n", release.Date, release.Channel, release.Status)
	} else {
		fmt.Fprintf(&b, "Release date: %s\n\nChannel: %s\n\nStatus: %s\n", release.Date, release.Channel, release.Status)
	}
	if len(release.Surfaces) > 0 {
		fmt.Fprintf(&b, "\n%s: %s\n", heading("Surfaces", "涉及产品面"), strings.Join(release.Surfaces, ", "))
	}

	fmt.Fprintf(&b, "\n## %s\n\n%s\n", heading("Summary", "摘要"), localize(release.Summary))
	if release.TargetingVersion == 1 {
		fmt.Fprintf(
			&b,
			"\n**Desktop %d · CLI %d**\n",
			releaseTargetCount(release, "desktop"),
			releaseTargetCount(release, "cli"),
		)
		renderReleaseTargetSection(&b, release, "desktop", heading("Desktop updates", "桌面端更新"), localize, heading)
		renderReleaseTargetSection(&b, release, "cli", heading("CLI updates", "CLI 端更新"), localize, heading)
		renderOtherReleaseUpdates(&b, release, localize, heading)
	} else {
		renderReleaseItems(&b, heading("Highlights", "亮点"), release.Highlights, localize, heading)
		if len(release.Changes.New)+len(release.Changes.Improved)+len(release.Changes.Fixed) > 0 {
			fmt.Fprintf(&b, "\n## %s\n", heading("Changes", "变更"))
			renderReleaseItemGroup(&b, heading("New", "新增"), release.Changes.New, localize, heading)
			renderReleaseItemGroup(&b, heading("Improved", "改进"), release.Changes.Improved, localize, heading)
			renderReleaseItemGroup(&b, heading("Fixed", "修复"), release.Changes.Fixed, localize, heading)
		}
	}
	renderReleaseItems(&b, heading("Upgrade notes", "升级说明"), release.Upgrade, localize, heading)
	renderReleaseItems(&b, heading("Known risks", "已知风险"), release.Risks, localize, heading)
	renderReleaseItems(&b, heading("Related guides", "相关指南"), release.Guides, localize, heading)
	if len(release.Contributors) > 0 {
		fmt.Fprintf(&b, "\n## %s\n\n%s\n", heading("Contributors", "贡献者"), strings.Join(release.Contributors, ", "))
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func releaseItems(release releaseRecord) []releaseItem {
	items := make([]releaseItem, 0, len(release.Highlights)+len(release.Changes.New)+len(release.Changes.Improved)+len(release.Changes.Fixed)+len(release.Upgrade)+len(release.Risks))
	items = append(items, release.Highlights...)
	items = append(items, release.Changes.New...)
	items = append(items, release.Changes.Improved...)
	items = append(items, release.Changes.Fixed...)
	items = append(items, release.Upgrade...)
	items = append(items, release.Risks...)
	return items
}

func releaseItemHasTarget(item releaseItem, target string) bool {
	return slices.Contains(item.Targets, target)
}

func releaseTargetCount(release releaseRecord, target string) int {
	count := 0
	for _, item := range releaseItems(release) {
		if releaseItemHasTarget(item, target) {
			count++
		}
	}
	return count
}

func releaseItemsForTarget(items []releaseItem, target string) []releaseItem {
	filtered := make([]releaseItem, 0, len(items))
	for _, item := range items {
		if releaseItemHasTarget(item, target) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func renderReleaseTargetSection(
	b *strings.Builder,
	release releaseRecord,
	target string,
	title string,
	localize func(localizedText) string,
	heading func(string, string) string,
) {
	fmt.Fprintf(b, "\n## %s\n", title)
	highlights := releaseItemsForTarget(release.Highlights, target)
	newItems := releaseItemsForTarget(release.Changes.New, target)
	improved := releaseItemsForTarget(release.Changes.Improved, target)
	fixed := releaseItemsForTarget(release.Changes.Fixed, target)
	renderReleaseItemGroup(b, heading("Highlights", "重点内容"), highlights, localize, heading)
	renderReleaseItemGroup(b, heading("New", "新增"), newItems, localize, heading)
	renderReleaseItemGroup(b, heading("Improved", "改进"), improved, localize, heading)
	renderReleaseItemGroup(b, heading("Fixed", "修复"), fixed, localize, heading)
	if len(highlights)+len(newItems)+len(improved)+len(fixed) > 0 {
		return
	}
	if target == "cli" && releaseTargetCount(release, target) == 0 {
		fmt.Fprintf(b, "\n%s\n", heading(
			"This release has no CLI features, improvements, or fixes; CLI users may skip it if preferred.",
			"本版本没有 CLI 相关功能、改进或修复，CLI 用户可按需跳过。",
		))
		return
	}
	fmt.Fprintf(b, "\n%s\n", heading(
		"No feature or fix entries are listed here; see the upgrade and risk notes below.",
		"此板块没有功能或修复条目；相关说明请查看下方升级提醒和风险提示。",
	))
}

func otherOnlyReleaseItems(items []releaseItem) []releaseItem {
	filtered := make([]releaseItem, 0, len(items))
	for _, item := range items {
		if len(item.Targets) > 0 && !releaseItemHasTarget(item, "desktop") && !releaseItemHasTarget(item, "cli") {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func renderOtherReleaseUpdates(
	b *strings.Builder,
	release releaseRecord,
	localize func(localizedText) string,
	heading func(string, string) string,
) {
	items := otherOnlyReleaseItems(release.Highlights)
	items = append(items, otherOnlyReleaseItems(release.Changes.New)...)
	items = append(items, otherOnlyReleaseItems(release.Changes.Improved)...)
	items = append(items, otherOnlyReleaseItems(release.Changes.Fixed)...)
	if len(items) == 0 {
		return
	}
	renderReleaseItems(b, heading("Other project updates", "其他项目更新"), items, localize, heading)
}

func renderReleaseItems(
	b *strings.Builder,
	title string,
	items []releaseItem,
	localize func(localizedText) string,
	heading func(string, string) string,
) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n", title)
	renderReleaseItemBullets(b, items, localize, heading)
}

func renderReleaseItemGroup(
	b *strings.Builder,
	title string,
	items []releaseItem,
	localize func(localizedText) string,
	heading func(string, string) string,
) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n### %s\n", title)
	renderReleaseItemBullets(b, items, localize, heading)
}

func renderReleaseItemBullets(
	b *strings.Builder,
	items []releaseItem,
	localize func(localizedText) string,
	heading func(string, string) string,
) {
	for _, item := range items {
		title := localize(item.Title)
		body := localize(item.Body)
		if title == "" && body == "" {
			continue
		}
		fmt.Fprintf(b, "\n- **%s**", title)
		if len(item.Targets) > 0 {
			labels := make([]string, 0, len(item.Targets))
			for _, target := range item.Targets {
				labels = append(labels, releaseTargetLabel(target, heading))
			}
			fmt.Fprintf(b, " [%s]", strings.Join(labels, " · "))
		}
		if item.Kind != "" {
			fmt.Fprintf(b, " [%s]", item.Kind)
		} else if item.Level != "" {
			fmt.Fprintf(b, " [%s]", item.Level)
		}
		if body != "" {
			fmt.Fprintf(b, ": %s", body)
		}
		if item.Href != "" {
			fmt.Fprintf(b, " [%s](%s)", heading("Open guide", "打开指南"), item.Href)
		}
		if len(item.Refs) > 0 {
			refs := make([]string, 0, len(item.Refs))
			for _, ref := range item.Refs {
				refs = append(refs, fmt.Sprintf("#%d", ref))
			}
			fmt.Fprintf(b, " (%s: %s)", heading("Refs", "关联"), strings.Join(refs, ", "))
		}
		b.WriteByte('\n')
	}
}

func releaseTargetLabel(target string, heading func(string, string) string) string {
	switch target {
	case "desktop":
		return heading("Desktop", "桌面端")
	case "cli":
		return "CLI"
	case "site":
		return heading("Site", "网站")
	case "service":
		return heading("Service", "服务端")
	default:
		return target
	}
}
