package tree_sitter

type LogType int

const (
	LogTypeParse LogType = iota
	LogTypeLex
)

// A callback that receives log messages during parser.
type Logger = func(LogType, string)
