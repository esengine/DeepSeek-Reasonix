package team

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"reasonix/internal/base/fileutil"
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
	if r := []rune(purpose); len(r) > teamroster.MaxSecretPurposeRunes {
		purpose = string(r[:teamroster.MaxSecretPurposeRunes])
	}
	held := false
	err := s.store.EditSecrets(ctx, func(b *teamroster.SecretBook) error {
		if i := slices.IndexFunc(b.Secrets, func(sc teamroster.Secret) bool { return sc.Name == n }); i >= 0 && allowed(b.Secrets[i], c.Agent) {
			held = true
			return nil
		}
		b.Requests = slices.DeleteFunc(b.Requests, func(q teamroster.SecretRequest) bool { return q.Name == n && q.Agent == c.Agent })
		if mine := slices.IndexFunc(b.Requests, func(q teamroster.SecretRequest) bool { return q.Agent == c.Agent }); mine >= 0 &&
			countRequests(b.Requests, c.Agent) >= teamroster.MaxSecretRequests {
			b.Requests = slices.Delete(b.Requests, mine, mine+1)
		}
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

func countRequests(qs []teamroster.SecretRequest, agent string) int {
	n := 0
	for _, q := range qs {
		if q.Agent == agent {
			n++
		}
	}
	return n
}

func allowed(sc teamroster.Secret, agent string) bool {
	return slices.Contains(sc.Agents, agent)
}

// UserSaveSecret keeps a value the user typed for an explicit, nonempty list
// of agents. It closes every
// open request for the name and tells each asker whether it was given the
// secret and how to use it — never what it is. A replaced value is retired.
func (s *Service) UserSaveSecret(ctx context.Context, name, value, purpose string, agents []string, requester string) error {
	defer s.changed()
	n := teamroster.NormalizeSecretName(name)
	if n == "" {
		return &teamroster.FieldError{Field: "name", Err: teamroster.ErrInvalid}
	}
	if len(agents) == 0 {
		return &teamroster.FieldError{Field: "agents", Err: teamroster.ErrEmpty}
	}
	r, err := s.store.Load()
	if err != nil {
		return err
	}
	for _, a := range append(slices.Clone(agents), requester) {
		if _, ok := r.Agent(a); a != "" && !ok {
			return teamroster.ErrUnknownAgent
		}
	}
	saved := teamroster.Secret{Name: n, Value: value, Purpose: strings.TrimSpace(purpose), Agents: agents, RequestedBy: requester}
	var askers []string
	if requester != "" {
		askers = append(askers, requester)
	}
	err = s.store.EditSecrets(ctx, func(b *teamroster.SecretBook) error {
		now := s.now()
		saved.CreatedAt, saved.UpdatedAt = now, now
		if i := slices.IndexFunc(b.Secrets, func(x teamroster.Secret) bool { return x.Name == n }); i >= 0 {
			if old := b.Secrets[i]; old.Value != value {
				b.Retire(n, old.Value, now, old.Slot)
			}
			saved.CreatedAt = b.Secrets[i].CreatedAt
			b.Secrets[i] = saved
		} else {
			b.Secrets = append(b.Secrets, saved)
		}
		b.Requests = slices.DeleteFunc(b.Requests, func(q teamroster.SecretRequest) bool {
			if q.Name == n && !slices.Contains(askers, q.Agent) {
				askers = append(askers, q.Agent)
			}
			return q.Name == n
		})
		return nil
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, a := range askers {
		if _, ok := r.Agent(a); !ok {
			continue
		}
		if !allowed(saved, a) {
			errs = append(errs, s.tellSecret(ctx, a, n, "withheld", fmt.Sprintf("The user saved %s, but did not give it to you: your commands do not receive it. Say what you cannot do without it.", n)))
			continue
		}
		errs = append(errs, s.tellSecret(ctx, a, n, "saved", fmt.Sprintf("The user saved %s for you. Shell commands read it as $%s%s. You cannot see its value; never print it, write it to a file, or send it anywhere it was not meant for.", n, SecretEnvPrefix, n)))
	}
	return errors.Join(errs...)
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

// UserDeleteSecret stops a secret reaching commands. Its value is retired, so
// the copies already in conversations stay masked.
func (s *Service) UserDeleteSecret(ctx context.Context, name string) error {
	defer s.changed()
	return s.store.EditSecrets(ctx, func(b *teamroster.SecretBook) error {
		i := slices.IndexFunc(b.Secrets, func(x teamroster.Secret) bool { return x.Name == name })
		if i < 0 {
			return teamroster.ErrUnknownSecret
		}
		b.Retire(name, b.Secrets[i].Value, s.now(), b.Secrets[i].Slot)
		b.Secrets = slices.Delete(b.Secrets, i, i+1)
		return nil
	})
}

// UserForgetRetired drops every retired value from the file. This process
// keeps masking what it already read until it exits.
func (s *Service) UserForgetRetired(ctx context.Context) error {
	defer s.changed()
	var slots []string
	err := s.store.EditSecrets(ctx, func(b *teamroster.SecretBook) error {
		for _, r := range b.Retired {
			if r.Slot != "" && !slices.ContainsFunc(b.Secrets, func(sc teamroster.Secret) bool { return sc.Slot == r.Slot }) {
				slots = append(slots, r.Slot)
			}
		}
		b.Retired = nil
		return nil
	})
	if err != nil {
		return err
	}
	if remover, ok := s.store.Credentials.(interface{ Remove(string) error }); ok {
		var errs []error
		for _, slot := range slices.Compact(slots) {
			errs = append(errs, remover.Remove(slot))
		}
		return errors.Join(errs...)
	}
	return nil
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

// WatchSecrets makes the team's secret file a secret path and the source of
// what this process masks: every value stored or retired there, read again on
// each snapshot, so a value another process saved is masked here too.
func WatchSecrets(store teamroster.Store) {
	store = WithSecretCredentials(store)
	secrets.RegisterHostSecretPath(store.SecretPath())
	var cache fileutil.ReadCache[teamroster.SecretBook]
	secrets.WatchValues(store.SecretPath(), func() []secrets.Labeled {
		metadata, err := cache.Load(store.SecretPath(), func(string) (teamroster.SecretBook, error) { return store.SecretMetadata() })
		if err != nil {
			return nil
		}
		book, _ := store.ResolveSecretMetadata(metadata)
		out := make([]secrets.Labeled, 0, len(book.Secrets)+len(book.Retired))
		for _, sc := range book.Secrets {
			out = append(out, secrets.Labeled{Label: sc.Name, Value: sc.Value})
		}
		for _, r := range book.Retired {
			out = append(out, secrets.Labeled{Label: r.Name, Value: r.Value})
		}
		return out
	})
	meta, err := store.SecretMetadata()
	if err != nil {
		return
	}
	legacy := slices.ContainsFunc(meta.Secrets, func(sc teamroster.Secret) bool { return sc.Value != "" && sc.Slot == "" }) || slices.ContainsFunc(meta.Retired, func(sc teamroster.RetiredSecret) bool { return sc.Value != "" && sc.Slot == "" })
	if legacy {
		_ = secrets.Masker()
		if err := store.EditSecrets(context.Background(), func(*teamroster.SecretBook) error { return nil }); err != nil {
			slog.Warn("team: credential migration did not complete; legacy values remain masked", "err", err)
		}
	}
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
