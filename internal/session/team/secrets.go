package team

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/hostframe"
	"reasonix/internal/state/teamroster"
)

// TagSecret is the block that tells an agent what became of a secret it asked for.
const TagSecret = "secret-update"

// SecretEnvPrefix starts the environment variable a secret reaches commands under.
const SecretEnvPrefix = "REASONIX_SECRET_"

// RequestSecret asks the user for a secret, from the agent's own conversation:
// a group's members would all read the answer. The card the user fills in is
// drawn from the call itself; the agent hears back when they save or decline.
func (s *Service) RequestSecret(ctx context.Context, c Caller, name, purpose string) (string, error) {
	if c.Group != "" {
		return "", ErrPrivateOnly
	}
	n := teamroster.NormalizeSecretName(name)
	if n == "" {
		return "", &teamroster.FieldError{Field: "name", Err: teamroster.ErrInvalid}
	}
	purpose = strings.TrimSpace(purpose)
	held := false
	err := s.store.EditSecrets(ctx, func(b *teamroster.SecretBook) error {
		if i := slices.IndexFunc(b.Secrets, func(sc teamroster.Secret) bool { return sc.Name == n }); i >= 0 && b.Secrets[i].Value != "" && allowed(b.Secrets[i], c.Agent) {
			held = true
			return nil
		}
		b.Requests = slices.DeleteFunc(b.Requests, func(q teamroster.SecretRequest) bool { return q.Name == n && q.Agent == c.Agent })
		b.Requests = append(b.Requests, teamroster.SecretRequest{Name: n, Purpose: purpose, Agent: c.Agent, At: s.now()})
		return nil
	})
	if err != nil {
		return "", err
	}
	s.changed()
	if held {
		return fmt.Sprintf("The user already saved %s for you. Shell commands read it as $%s%s; you cannot see its value, and must never print it.", n, SecretEnvPrefix, n), nil
	}
	return fmt.Sprintf("Asked the user for %s. End your turn now: they are shown a card to fill it in, and you get a message when they save or decline it. You never see the value; once saved, shell commands read it as $%s%s.", n, SecretEnvPrefix, n), nil
}

func allowed(sc teamroster.Secret, agent string) bool {
	return slices.Contains(sc.Agents, agent)
}

// UserSaveSecret keeps a value the user typed for explicitly named agents,
// closes the request for it, and tells the asking agent how to
// use it — never what it is.
func (s *Service) UserSaveSecret(ctx context.Context, name, value, purpose string, agents []string, requester string) error {
	defer s.changed()
	if len(agents) == 0 {
		return &teamroster.FieldError{Field: "agents", Err: teamroster.ErrEmpty}
	}
	n := teamroster.NormalizeSecretName(name)
	if n == "" {
		return &teamroster.FieldError{Field: "name", Err: teamroster.ErrInvalid}
	}
	r, err := s.store.Load()
	if err != nil {
		return err
	}
	for _, a := range agents {
		if _, ok := r.Agent(a); !ok {
			return teamroster.ErrUnknownAgent
		}
	}
	var old string
	err = s.store.EditSecrets(ctx, func(b *teamroster.SecretBook) error {
		now := s.now()
		sc := teamroster.Secret{Name: n, Value: value, Purpose: strings.TrimSpace(purpose), Agents: agents, RequestedBy: requester, CreatedAt: now, UpdatedAt: now}
		if i := slices.IndexFunc(b.Secrets, func(x teamroster.Secret) bool { return x.Name == n }); i >= 0 {
			old, sc.CreatedAt = b.Secrets[i].Value, b.Secrets[i].CreatedAt
			b.Secrets[i] = sc
		} else {
			b.Secrets = append(b.Secrets, sc)
		}
		b.Requests = slices.DeleteFunc(b.Requests, func(q teamroster.SecretRequest) bool { return q.Name == n })
		return nil
	})
	if err != nil {
		return err
	}
	if old != "" && old != value {
		secrets.UnregisterValue(old)
	}
	secrets.RegisterValue(n, value)
	scope := "every agent on the team"
	if len(agents) > 0 {
		scope = "the agents the user chose"
	}
	return s.tellSecret(ctx, requester, n, "saved", fmt.Sprintf("The user saved %s for %s. Shell commands read it as $%s%s. You cannot see its value; never print it, write it to a file, or send it anywhere it was not meant for.", n, scope, SecretEnvPrefix, n))
}

// UserCancelSecret closes a request the user declined, and tells its agent.
func (s *Service) UserCancelSecret(ctx context.Context, name, agent string) error {
	defer s.changed()
	n := teamroster.NormalizeSecretName(name)
	if err := s.store.EditSecrets(ctx, func(b *teamroster.SecretBook) error {
		b.Requests = slices.DeleteFunc(b.Requests, func(q teamroster.SecretRequest) bool { return q.Name == n && q.Agent == agent })
		return nil
	}); err != nil {
		return err
	}
	return s.tellSecret(ctx, agent, n, "declined", fmt.Sprintf("The user declined to give %s. Do not ask for it again unless they bring it up; say what you cannot do without it.", n))
}

// UserDeleteSecret removes a secret; commands no longer receive it.
func (s *Service) UserDeleteSecret(ctx context.Context, name string) error {
	defer s.changed()
	var gone string
	err := s.store.EditSecrets(ctx, func(b *teamroster.SecretBook) error {
		i := slices.IndexFunc(b.Secrets, func(x teamroster.Secret) bool { return x.Name == name })
		if i < 0 {
			return teamroster.ErrUnknownSecret
		}
		gone = b.Secrets[i].Value
		b.Secrets = slices.Delete(b.Secrets, i, i+1)
		return nil
	})
	if err == nil {
		secrets.UnregisterValue(gone)
	}
	return err
}

// SecretEnv is what an agent's shell commands receive: one variable per secret
// it was given. Read on every command, so a secret saved or removed since the
// session opened counts at once.
func (s *Service) SecretEnv(agent string) []string {
	book, err := s.store.Secrets()
	if err != nil {
		return nil
	}
	var out []string
	for _, sc := range book.Secrets {
		if allowed(sc, agent) {
			out = append(out, SecretEnvPrefix+sc.Name+"="+sc.Value)
		}
	}
	return out
}

// RegisterSecrets marks every value stored for the team for masking, and the
// file they live in as a secret path. A host calls it at start, before any
// session is built, whether or not it holds the team's writer lease.
func RegisterSecrets(store teamroster.Store) error {
	store = WithSecretCredentials(store)
	secrets.RegisterHostSecretPath(store.SecretPath())
	meta, err := store.SecretMetadata()
	if err != nil {
		return err
	}
	for _, sc := range meta.Secrets {
		secrets.RegisterValue(sc.Name, sc.Value)
	}
	if slices.ContainsFunc(meta.Secrets, func(sc teamroster.Secret) bool { return sc.Slot == "" && sc.Value != "" }) {
		if err := store.EditSecrets(context.Background(), func(*teamroster.SecretBook) error { return nil }); err != nil {
			return err
		}
	}
	book, err := store.Secrets()
	for _, sc := range book.Secrets {
		secrets.RegisterValue(sc.Name, sc.Value)
	}
	return err
}

func (s *Service) tellSecret(ctx context.Context, agent, name, status, note string) error {
	if agent == "" {
		return nil
	}
	pane, err := s.panes.Ensure(ctx, SeatRef{Agent: agent})
	if err != nil {
		return err
	}
	text := hostframe.Block(TagSecret, []hostframe.Attr{{Name: "name", Value: name}, {Name: "status", Value: status}}, note)
	return pane.Deliver(Delivery{Key: fmt.Sprintf("secret-%s-%s-%d", name, status, s.now().UnixNano()), Kind: DeliverSecret, Display: "secret " + name + " " + status, Text: text})
}
