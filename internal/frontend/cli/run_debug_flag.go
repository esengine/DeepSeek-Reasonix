package cli

import (
	"log/slog"
	"strconv"
)

// debugLogFlag is `run --debug`. It lowers the process log level while the
// flags are parsed, which is before assembly writes its first record.
type debugLogFlag bool

func (d *debugLogFlag) String() string { return strconv.FormatBool(bool(*d)) }

func (d *debugLogFlag) Type() string { return "bool" }

func (d *debugLogFlag) IsBoolFlag() bool { return true }

func (d *debugLogFlag) Set(value string) error {
	on, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	*d = debugLogFlag(on)
	if on {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
	return nil
}
