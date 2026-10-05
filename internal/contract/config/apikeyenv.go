// apikeyenv.go — where a provider's key is stored, derived from its name.
package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// APIKeyEnvFor names the credential slot a provider's key lives in, derived
// from the provider name so two never share one. It sits beside isCredentialKey
// because building a name and judging one are the same rule: a second copy
// elsewhere produced "129_API_KEY" for a relay called "129", which the store
// then refused with nothing the person could act on.
func APIKeyEnvFor(name string) string {
	stem := strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, strings.ToUpper(strings.TrimSpace(name)))
	stem = strings.Trim(stem, "_")
	if stem == "" {
		return "CUSTOM_" + fnv1a32Hex(name) + "_API_KEY"
	}
	// An environment variable may not open with a digit, and a name that does
	// is ordinary for a self-hosted relay.
	if stem[0] >= '0' && stem[0] <= '9' {
		stem = "CUSTOM_" + stem
	}
	return stem + "_API_KEY"
}

// FreeAPIKeyEnvFor names the slot for a new provider among providers. Names
// fold (case, punctuation), and a removed provider leaves its credential stored,
// so the name's own slot is used only while no entry holds it and nothing is
// stored there; otherwise the key gets a private slot of its own.
func FreeAPIKeyEnvFor(name string, providers []ProviderEntry) (string, error) {
	free := func(slot string) bool {
		for i := range providers {
			if strings.TrimSpace(providers[i].APIKeyEnv) == slot {
				return false
			}
		}
		return !CredentialStored(slot)
	}
	if own := APIKeyEnvFor(name); free(own) {
		return own, nil
	}
	for {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return "", fmt.Errorf("allocate credential slot: %w", err)
		}
		if slot := fmt.Sprintf("%s%X%s", privateSlotPrefix, id, privateSlotSuffix); free(slot) {
			return slot, nil
		}
	}
}

const (
	privateSlotPrefix = "REASONIX_CONNECTION_"
	privateSlotSuffix = "_KEY"
	privateSlotIDHex  = 32
)

// IsPrivateCredentialSlot reports whether key is exactly a slot that
// FreeAPIKeyEnvFor allocates: the prefix, 32 upper-case hex digits, the suffix.
func IsPrivateCredentialSlot(key string) bool {
	id, ok := strings.CutPrefix(key, privateSlotPrefix)
	if !ok {
		return false
	}
	id, ok = strings.CutSuffix(id, privateSlotSuffix)
	if !ok || len(id) != privateSlotIDHex {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' && r < 'A' || r > 'F' {
			return false
		}
	}
	return true
}

func fnv1a32Hex(s string) string {
	hash := uint32(0x811c9dc5)
	for _, unit := range utf16.Encode([]rune(strings.TrimSpace(s))) {
		hash ^= uint32(unit)
		hash *= 0x01000193
	}
	return fmt.Sprintf("%08x", hash)
}

// ErrInvalidCredentialKey is refused when a slot name is not one an environment
// variable may carry. Callers tell it apart to say which field the person has
// to change: the name a slot is derived from, never the key they pasted.
var ErrInvalidCredentialKey = errors.New("credential slot name is not a usable environment variable")
