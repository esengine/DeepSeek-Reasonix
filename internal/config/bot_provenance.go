package config

// BotInstallMayEnableGateway reports whether a finished bot install may switch
// the embedded gateway on. The first connection is onboarding and always may;
// after that an explicit [bot] enabled is the user's, so re-scanning a QR code
// to refresh credentials must not turn a deliberate false back on (#8041).
func BotInstallMayEnableGateway(c *Config) bool {
	if c == nil {
		return false
	}
	return len(c.Bot.Connections) == 0 || !tomlFileDefinesKey(UserConfigPath(), "bot", "enabled")
}
