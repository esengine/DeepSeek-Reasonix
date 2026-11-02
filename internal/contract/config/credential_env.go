package config

import "os"

func pinCredentialAssignments(assignments map[string]string) {
	for key, value := range assignments {
		if IsHostCredentialSlot(key) {
			continue
		}
		_ = os.Setenv(key, value)
		recordCredentialSource(key, value, CredentialSource{Kind: CredentialSourceCredentials, Path: UserCredentialsPath(), Label: "Reasonix credentials (.env)"})
	}
}
