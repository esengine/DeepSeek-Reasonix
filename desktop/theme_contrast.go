package main

import (
	"math"
	"strconv"
	"strings"
)

// computeContrastWarnings checks common text/background pairs for WCAG AA.
// Warnings never block save — the UI only surfaces them.
func computeContrastWarnings(m *ThemePackManifest) []ThemeContrastWarning {
	if m == nil {
		return nil
	}
	var out []ThemeContrastWarning
	out = append(out, contrastForMode("light", m.Tokens.Light)...)
	out = append(out, contrastForMode("dark", m.Tokens.Dark)...)
	return out
}

func contrastForMode(mode string, tokens map[string]string) []ThemeContrastWarning {
	if len(tokens) == 0 {
		return nil
	}
	// Defaults used when only one side of a pair is overridden.
	defFG, defBG, defFaint, defChat, defAccent, defAccentFG := defaultContrastColors(mode)

	fg := firstToken(tokens, "fg", defFG)
	bg := firstToken(tokens, "bg", defBG)
	faint := firstToken(tokens, "fgFaint", defFaint)
	chat := firstToken(tokens, "chat", defChat)
	accent := firstToken(tokens, "accent", defAccent)
	accentFG := firstToken(tokens, "accentFg", defAccentFG)

	var out []ThemeContrastWarning
	// Body text on main background — AA normal text requires 4.5:1.
	if r, ok := contrastRatio(fg, bg); ok && r < 4.5 {
		out = append(out, ThemeContrastWarning{
			Mode: mode, Pair: "fg/bg", Ratio: round2(r), Minimum: 4.5,
			Suggest: suggestFGForBG(bg, mode),
		})
	}
	if r, ok := contrastRatio(fg, chat); ok && r < 4.5 {
		out = append(out, ThemeContrastWarning{
			Mode: mode, Pair: "fg/chat", Ratio: round2(r), Minimum: 4.5,
			Suggest: suggestFGForBG(chat, mode),
		})
	}
	if r, ok := contrastRatio(faint, bg); ok && r < 3.0 {
		out = append(out, ThemeContrastWarning{
			Mode: mode, Pair: "fgFaint/bg", Ratio: round2(r), Minimum: 3.0,
			Suggest: suggestFGForBG(bg, mode),
		})
	}
	if r, ok := contrastRatio(accentFG, accent); ok && r < 3.0 {
		out = append(out, ThemeContrastWarning{
			Mode: mode, Pair: "accentFg/accent", Ratio: round2(r), Minimum: 3.0,
			Suggest: suggestFGForBG(accent, mode),
		})
	}
	return out
}

func defaultContrastColors(mode string) (fg, bg, faint, chat, accent, accentFG string) {
	if mode == "light" {
		return "#111827", "#f7f8fb", "#8a94a6", "#ffffff", "#2f5fa8", "#ffffff"
	}
	return "#f1f1ef", "#0c0d10", "#6c6e74", "#0c0d10", "#ff6a3d", "#0c0d10"
}

func firstToken(tokens map[string]string, key, def string) string {
	if v, ok := tokens[key]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func contrastRatio(a, b string) (float64, bool) {
	la, ok1 := relativeLuminance(a)
	lb, ok2 := relativeLuminance(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	lighter := math.Max(la, lb)
	darker := math.Min(la, lb)
	return (lighter + 0.05) / (darker + 0.05), true
}

func relativeLuminance(hex string) (float64, bool) {
	r, g, b, ok := parseHexRGB(hex)
	if !ok {
		return 0, false
	}
	return 0.2126*srgbLin(r) + 0.7152*srgbLin(g) + 0.0722*srgbLin(b), true
}

func srgbLin(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func parseHexRGB(hex string) (r, g, b float64, ok bool) {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 6 && len(hex) != 8 {
		return 0, 0, 0, false
	}
	ri, err1 := strconv.ParseUint(hex[0:2], 16, 8)
	gi, err2 := strconv.ParseUint(hex[2:4], 16, 8)
	bi, err3 := strconv.ParseUint(hex[4:6], 16, 8)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	return float64(ri) / 255, float64(gi) / 255, float64(bi) / 255, true
}

func suggestFGForBG(bg string, mode string) string {
	// Simple suggestion: pick near-black or near-white based on background luminance.
	if lum, ok := relativeLuminance(bg); ok {
		if lum > 0.5 {
			return "#111827"
		}
		return "#f1f1ef"
	}
	if mode == "light" {
		return "#111827"
	}
	return "#f1f1ef"
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
