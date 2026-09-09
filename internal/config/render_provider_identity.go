package config

import (
	"fmt"
	"strings"
)

func renderProviderIdentity(b *strings.Builder, keyEnv, displayName string) {
	fmt.Fprintf(b, "api_key_env = %q\n", keyEnv)
	if displayName != "" {
		fmt.Fprintf(b, "display_name = %q\n", displayName)
	}
}
