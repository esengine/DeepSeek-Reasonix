package config

import (
	"errors"
	"fmt"
	"strings"
)

// ProgressWatchConfig says when a run reads as no longer moving. It is
// user-global: a repository's reasonix.toml cannot pause the user's runs or
// quiet the notice, so load restores the user's value over a project's.
type ProgressWatchConfig struct {
	Pause         bool `toml:"pause"`          // end a stalled run resumably; off only says so
	Rounds        int  `toml:"rounds"`         // idle tool rounds; zero is the default
	TokenMultiple int  `toml:"token_multiple"` // input backstop in context windows; zero is the default
	// PerseverationRetries is the loop-guard budget: unset or 0 reports a
	// detected loop, >0 also cuts and nudges, <0 disables the guard.
	PerseverationRetries *int `toml:"perseveration_retries"`
}

const (
	DefaultProgressWatchRounds        = 20
	DefaultProgressWatchTokenMultiple = 8
	maxProgressWatchRounds            = 1000
	maxProgressWatchTokenMultiple     = 1000
)

// ErrProgressWatchOutOfRange marks a setting outside what the watch accepts.
var ErrProgressWatchOutOfRange = errors.New("progress watch setting out of range")

// ProgressWatchRounds is the effective round limit.
func (c *Config) ProgressWatchRounds() int {
	if c == nil || c.ProgressWatch.Rounds <= 0 {
		return DefaultProgressWatchRounds
	}
	return c.ProgressWatch.Rounds
}

// ProgressWatchTokenMultiple is the effective token backstop multiple.
func (c *Config) ProgressWatchTokenMultiple() int {
	if c == nil || c.ProgressWatch.TokenMultiple <= 0 {
		return DefaultProgressWatchTokenMultiple
	}
	return c.ProgressWatch.TokenMultiple
}

// SetProgressWatch validates and stores the watch. Zero keeps a limit at its
// default, so a caller cannot switch a judgement off by leaving it blank.
func (c *Config) SetProgressWatch(w ProgressWatchConfig) error {
	if w.Rounds < 0 || w.Rounds > maxProgressWatchRounds {
		return fmt.Errorf("%w: rounds must be between 1 and %d", ErrProgressWatchOutOfRange, maxProgressWatchRounds)
	}
	if w.TokenMultiple < 0 || w.TokenMultiple > maxProgressWatchTokenMultiple {
		return fmt.Errorf("%w: token_multiple must be between 1 and %d", ErrProgressWatchOutOfRange, maxProgressWatchTokenMultiple)
	}
	c.ProgressWatch = w
	return nil
}

// renderProgressWatchSection writes the section only once the user has said
// something in it; an untouched install keeps a config without it.
func renderProgressWatchSection(b *strings.Builder, c *Config) {
	if c.ProgressWatch == (ProgressWatchConfig{}) {
		return
	}
	b.WriteString("[progress_watch]\n")
	fmt.Fprintf(b, "pause = %v   # end a run that stops producing observable effects; resumable, default off\n", c.ProgressWatch.Pause)
	fmt.Fprintf(b, "rounds = %d   # tool rounds without a file change, check moving, task step, new read or delegated result\n", c.ProgressWatchRounds())
	fmt.Fprintf(b, "token_multiple = %d   # also when input spent since the last observable effect reaches this many context windows\n", c.ProgressWatchTokenMultiple())
	renderPerseverationRetries(b, c.ProgressWatch.PerseverationRetries, nil)
	b.WriteString("\n")
}
