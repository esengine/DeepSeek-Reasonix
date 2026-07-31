package tomltest

type versionSpec struct {
	inherit string
	exclude []string
}

var versions = map[string]versionSpec{
	"1.1.0": versionSpec{
		exclude: []string{
			"valid/spec-1.0.0/*",
			"invalid/spec-1.0.0/*",
			"invalid/datetime/no-secs", // Times without seconds is no longer invalid.
			"invalid/local-time/no-secs",
			"invalid/local-datetime/no-secs",
			"invalid/string/basic-byte-escapes", // \x is now valid.
			"invalid/inline-table/trailing-comma",
			"invalid/inline-table/linebreak-01",
			"invalid/inline-table/linebreak-02",
			"invalid/inline-table/linebreak-03",
			"invalid/inline-table/linebreak-04",
		},
	},

	"1.0.0": versionSpec{
		exclude: []string{
			"valid/spec-1.1.0/*",
			"invalid/spec-1.1.0/*",
			"valid/string/escape-esc",                               // \e
			"valid/string/hex-escape", "invalid/string/bad-hex-esc", // \x..
			"valid/datetime/no-seconds", // Times without seconds
			"valid/inline-table/newline",
			"valid/inline-table/newline-comment",
		},
	},
}
