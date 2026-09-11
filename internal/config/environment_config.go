package config

// EnvironmentConfig controls startup environment reporting and network facts.
type EnvironmentConfig struct {
	Enabled *bool             `toml:"enabled"`
	Offline bool              `toml:"offline"`
	Tools   map[string]string `toml:"tools"`
}
