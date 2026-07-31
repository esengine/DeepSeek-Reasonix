package types

import "time"

// SafetyConfig holds safety and performance pipeline configurations.
type SafetyConfig struct {
	Dedup struct {
		MaxEntries      int
		SweepIntervalMs time.Duration
	}
	Batch                BatchConfig
	StaleMessageWindowMs time.Duration
}

// OutboundConfig holds outbound sending behavior configurations.
type OutboundConfig struct {
	TextChunkLimit      int
	StreamThrottleMs    time.Duration
	StreamThrottleChars int
	Retry               struct {
		MaxAttempts int
		BaseDelayMs time.Duration
	}
}

// BotIdentityCacheConfig controls how bot identity is cached and refreshed.
type BotIdentityCacheConfig struct {
	TTL                time.Duration
	MinRefreshInterval time.Duration
}

// ChannelConfig is the consolidated configuration for the channel.
type ChannelConfig struct {
	Safety           SafetyConfig
	Policy           PolicyConfig
	Outbound         OutboundConfig
	BotIdentityCache BotIdentityCacheConfig
}

// ChannelOption is a function that modifies the ChannelConfig.
type ChannelOption func(*ChannelConfig)

// WithSafetyConfig sets the safety configuration.
func WithSafetyConfig(cfg SafetyConfig) ChannelOption {
	return func(c *ChannelConfig) {
		c.Safety = cfg
	}
}

// WithPolicyConfig sets the policy configuration.
func WithPolicyConfig(cfg PolicyConfig) ChannelOption {
	return func(c *ChannelConfig) {
		c.Policy = cfg
	}
}

// WithOutboundConfig sets the outbound configuration.
func WithOutboundConfig(cfg OutboundConfig) ChannelOption {
	return func(c *ChannelConfig) {
		c.Outbound = cfg
	}
}

// WithBotIdentityCacheConfig sets the bot identity cache configuration.
func WithBotIdentityCacheConfig(cfg BotIdentityCacheConfig) ChannelOption {
	return func(c *ChannelConfig) {
		c.BotIdentityCache = cfg
	}
}

// DefaultChannelConfig returns a default ChannelConfig to be used when no options are provided.
func DefaultChannelConfig() ChannelConfig {
	return ChannelConfig{
		Safety: SafetyConfig{
			Dedup: struct {
				MaxEntries      int
				SweepIntervalMs time.Duration
			}{
				MaxEntries:      10000,
				SweepIntervalMs: 1 * time.Hour,
			},
			Batch:                DefaultBatchConfig(),
			StaleMessageWindowMs: DefaultStaleWindow,
		},
		Policy: PolicyConfig{},
		Outbound: OutboundConfig{
			TextChunkLimit:      3500,
			StreamThrottleMs:    100 * time.Millisecond,
			StreamThrottleChars: 10,
			Retry: struct {
				MaxAttempts int
				BaseDelayMs time.Duration
			}{
				MaxAttempts: 3,
				BaseDelayMs: 500 * time.Millisecond,
			},
		},
		BotIdentityCache: BotIdentityCacheConfig{
			TTL:                30 * time.Minute,
			MinRefreshInterval: 1 * time.Minute,
		},
	}
}
