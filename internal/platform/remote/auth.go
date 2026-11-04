package remote

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/crypto/ssh"
)

// SecretKind identifies which interactive secret is being requested.
type SecretKind int

const (
	SecretPassphrase SecretKind = iota // private-key passphrase
	SecretPassword                     // password auth
)

func (k SecretKind) String() string {
	if k == SecretPassword {
		return "password"
	}
	return "passphrase"
}

// SecretPrompt obtains a one-shot credential without persisting or publishing
// it. Implementations should respect ctx cancellation when the connection is
// stopped or superseded.
type SecretPrompt func(ctx context.Context, kind SecretKind, host, identityFile string) (string, error)

// Challenge is one server-provided keyboard-interactive question.
// AnswerRequired is false for an instruction-only round.
type Challenge struct {
	Host           string
	Name           string
	Instruction    string
	Question       string
	Echo           bool
	AnswerRequired bool
}

// ChallengePrompt asks the user afresh; answers must never be cached or stored.
type ChallengePrompt func(context.Context, Challenge) (string, error)

// AuthOptions supplies credential resolution for a dial. Passphrase and
// Password return already-resolved credential-store values (nil when none is
// configured). SecretPrompt resolves reusable secrets; ChallengePrompt handles
// fresh server challenges on every connect, including reconnects.
type AuthOptions struct {
	Passphrase      func() (string, error)
	Password        func() (string, error)
	SecretPrompt    SecretPrompt
	ChallengePrompt ChallengePrompt
	DisableAgent    bool

	// cache holds secrets obtained during the first connect so the supervisor
	// can reconnect silently. Populated by the auth methods.
	cache *secretCache
}

type secretCache struct {
	passphrases map[string]string
	password    string
	havePw      bool
}

// buildAuthMethods assembles authentication in OpenSSH-like order: agent,
// explicit identity file (or default identities), password, then
// keyboard-interactive. Public-key sources are returned through an AuthCallback
// because x/crypto/ssh deliberately uses only the first static AuthMethod for a
// protocol method. Without the callback, an empty or rejected agent consumes
// "publickey" and the configured identity file is never attempted.
//
// Password methods are only offered when a stored credential or interactive
// prompt exists. Otherwise a rejected public key must remain a public-key
// authentication failure instead of being masked by a misleading "password
// required but no prompt available" callback error.
func buildAuthMethods(ctx context.Context, h ResolvedHost, opts *AuthOptions) ([]ssh.AuthMethod, ssh.ClientAuthCallback, func(), error) {
	if opts.cache == nil {
		opts.cache = &secretCache{}
	}
	var publicKeys []ssh.AuthMethod
	var fallback []ssh.AuthMethod
	cleanup := func() {}

	identityFiles := append([]string(nil), h.IdentityFiles...)
	if len(identityFiles) == 0 && h.IdentityFile != "" {
		identityFiles = []string{h.IdentityFile}
	}
	if len(identityFiles) == 0 && !h.IdentityFileNone {
		identityFiles = defaultIdentityFiles()
	}

	if !opts.DisableAgent {
		if am, closeAgent := agentAuth(identityFiles, h.IdentitiesOnly); am != nil {
			publicKeys = append(publicKeys, am)
			cleanup = closeAgent
		}
	}

	if len(identityFiles) > 0 {
		for _, identityFile := range identityFiles {
			am, err := keyAuth(ctx, h, opts, identityFile, len(identityFiles) > 1)
			if err != nil {
				// Preserve the old explicit-single-key behavior, but let an
				// OpenSSH identity list continue to its remaining candidates.
				if len(identityFiles) == 1 {
					cleanup()
					return nil, nil, func() {}, err
				}
				continue
			}
			if am != nil {
				publicKeys = append(publicKeys, am)
			}
		}
	}

	if opts.Password != nil || opts.SecretPrompt != nil {
		fallback = append(fallback, passwordAuth(ctx, h, opts))
	}
	if opts.ChallengePrompt != nil {
		fallback = append(fallback, keyboardInteractiveAuth(ctx, h, opts))
	}
	return fallback, publicKeyAuthCallback(publicKeys), cleanup, nil
}

// publicKeyAuthCallback returns each public-key source exactly once while the
// server continues to allow publickey authentication. AuthCallback may return
// multiple AuthMethod values with the same protocol name, unlike ClientConfig's
// static Auth slice.
func publicKeyAuthCallback(methods []ssh.AuthMethod) ssh.ClientAuthCallback {
	if len(methods) == 0 {
		return nil
	}
	next := 0
	return func(ctx *ssh.ClientAuthContext) (ssh.AuthMethod, error) {
		if next >= len(methods) || !containsAuthMethod(ctx.AllowedMethods, "publickey") {
			return nil, nil
		}
		method := methods[next]
		next++
		return method, nil
	}
}

func containsAuthMethod(methods []string, want string) bool {
	return slices.Contains(methods, want)
}

// keyAuth loads a private key, resolving a passphrase from the credential
// store then the interactive prompt when the key is encrypted. Returns nil
// (no method, no error) when the key file simply does not exist.
func keyAuth(ctx context.Context, h ResolvedHost, opts *AuthOptions, path string, allowDecryptSkip bool) (ssh.AuthMethod, error) {
	path = expandHome(path)
	pem, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(pem)
	if err == nil {
		return ssh.PublicKeys(signer), nil
	}
	// OpenSSH permits IdentityFile to name a public key when the matching
	// private key lives in ssh-agent. The filtered agent method above handles it.
	if _, _, _, _, publicErr := ssh.ParseAuthorizedKey(pem); publicErr == nil {
		return nil, nil
	}
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		return nil, fmt.Errorf("parse key %s: %w", path, err)
	}
	// Encrypted key: return a lazy method so the passphrase is only resolved
	// if the server actually offers publickey with this key.
	return ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
		pass, perr := resolvePassphrase(ctx, h, opts, path)
		if perr != nil {
			return nil, perr
		}
		s, serr := ssh.ParsePrivateKeyWithPassphrase(pem, []byte(pass))
		if serr != nil && opts.SecretPrompt != nil {
			// A host-level stored passphrase may unlock only one member of an
			// IdentityFile list. Give this identity its own one-shot prompt before
			// deciding that it is unavailable.
			delete(opts.cache.passphrases, path)
			pass, perr = opts.SecretPrompt(ctx, SecretPassphrase, h.Label(), path)
			if perr != nil {
				return nil, perr
			}
			opts.cache.passphrases[path] = pass
			s, serr = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(pass))
		}
		if serr != nil {
			delete(opts.cache.passphrases, path)
			// A configured identity list may contain encrypted keys with different
			// passphrases. Treat a failed decryption like an unavailable identity so
			// the next key can still be attempted; preserve the focused error for an
			// explicit single-key configuration.
			if allowDecryptSkip {
				return nil, nil
			}
			return nil, fmt.Errorf("decrypt key %s: %w", path, serr)
		}
		return []ssh.Signer{s}, nil
	}), nil
}

func resolvePassphrase(ctx context.Context, h ResolvedHost, opts *AuthOptions, identityFile string) (string, error) {
	if opts.cache.passphrases == nil {
		opts.cache.passphrases = map[string]string{}
	}
	if passphrase, ok := opts.cache.passphrases[identityFile]; ok {
		return passphrase, nil
	}
	if opts.Passphrase != nil {
		v, err := opts.Passphrase()
		if err != nil {
			return "", err
		}
		if v != "" {
			opts.cache.passphrases[identityFile] = v
			return v, nil
		}
	}
	if opts.SecretPrompt == nil {
		return "", fmt.Errorf("remote: key passphrase required but no prompt available")
	}
	v, err := opts.SecretPrompt(ctx, SecretPassphrase, h.Label(), identityFile)
	if err != nil {
		return "", err
	}
	opts.cache.passphrases[identityFile] = v
	return v, nil
}

func passwordAuth(ctx context.Context, h ResolvedHost, opts *AuthOptions) ssh.AuthMethod {
	return ssh.RetryableAuthMethod(ssh.PasswordCallback(func() (string, error) {
		return resolvePassword(ctx, h, opts)
	}), 3)
}

func keyboardInteractiveAuth(ctx context.Context, h ResolvedHost, opts *AuthOptions) ssh.AuthMethod {
	return ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) (answers []string, err error) {
		defer func() {
			if err != nil {
				err = fmt.Errorf("%w: %w", ErrAuthFailed, err)
			}
		}()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if opts.ChallengePrompt == nil {
			return nil, fmt.Errorf("remote: interactive challenge requires a prompt")
		}
		if len(questions) != len(echos) {
			return nil, fmt.Errorf("remote: invalid keyboard-interactive challenge")
		}
		q := Challenge{Host: h.Label(), Name: name, Instruction: instruction}
		answers = make([]string, len(questions))
		if len(questions) == 0 {
			_, err := opts.ChallengePrompt(ctx, q)
			return answers, err
		}
		q.AnswerRequired = true
		for i, question := range questions {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			q.Question, q.Echo = question, echos[i]
			answer, err := opts.ChallengePrompt(ctx, q)
			if err != nil {
				return nil, err
			}
			answers[i] = answer
		}
		return answers, ctx.Err()
	})
}

func resolvePassword(ctx context.Context, h ResolvedHost, opts *AuthOptions) (string, error) {
	if opts.cache.havePw {
		return opts.cache.password, nil
	}
	if opts.Password != nil {
		v, err := opts.Password()
		if err != nil {
			return "", err
		}
		if v != "" {
			opts.cache.password, opts.cache.havePw = v, true
			return v, nil
		}
	}
	if opts.SecretPrompt == nil {
		return "", fmt.Errorf("remote: password required but no prompt available")
	}
	v, err := opts.SecretPrompt(ctx, SecretPassword, h.Label(), "")
	if err != nil {
		return "", err
	}
	opts.cache.password, opts.cache.havePw = v, true
	return v, nil
}

func defaultIdentityFiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	names := []string{"id_ed25519", "id_ecdsa", "id_rsa"}
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.Join(home, ".ssh", n))
	}
	return out
}
