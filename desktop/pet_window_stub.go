//go:build !darwin

package main

// Stub implementations for non-macOS platforms.

type PetI18n struct {
	RunningBubble      string
	WaitingBubble      string
	ToolErrorBubble    string
	ErrorBubble        string
	Greetings          []string
	TaskDoneBubbles    []string
	Goodbyes           []string
	IdleBubbles        []string
	HeadTexts          []string
	BodyTexts          []string
	DragIdleText       string
	DragBusyText       string
	DblClickIdle0      string
	DblClickIdle1      string
	DblClickIdleMany   string
	DblClickRunning    string
	DblClickWaiting    string
	DblClickReview     string
	DblClickFailed     string
	DblClickFallback   string
	MenuTitle          string
	MenuSize           string
	MenuInstallPH      string
	MenuInstallBtn     string
	MenuRandomBtn      string
	MenuMarketBtn      string
	InstallDownloading string
	InstallSuccess     string
	InstallNotFound    string
	InstallNetworkErr  string
	InstallServerErr   string
	InstallParseErr    string
	InstallNoSprite    string
	InstallDlFailed    string
	InstallInvalidSlug string
	SpriteNotFound     string
	DeleteDone         string
	AlreadyInstalled   string
	Hello              string
}

func petI18n() PetI18n { return PetI18n{} }

func CreatePetWindow(homeDir string, scale float64, posX, posY int) {}

func PetEvaluateJS(script string) {}

func PetSetState(state, bubble string, sessionCount int) {}

func PetMoveBy(dx, dy float64) {}

func PetSetScale(scale float64) {}

func PetSetSlug(slug string) {}

func PetCloseWindow() {}

func petRefreshI18n() {} // stub

func petPopBubble(items []string) {} // stub

func PetEnabled() bool { return false }

func PetToggle() bool { return false }

func petIsDisabled() bool { return true }

func (a *App) AppPetToggle() bool { return false }

func (a *App) AppPetEnabled() bool { return false }
