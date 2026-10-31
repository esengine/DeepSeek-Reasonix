package teamroster

import "errors"

// SecretCredentials stores values outside the roster directory. Slots are
// immutable, so replacing a secret cannot change a retired value's identity.
type SecretCredentials interface {
	Save(value string) (slot string, err error)
	Read(slot string) (string, error)
}

var ErrSecretCredentials = errors.New("team secret credential store is unavailable")

func (s Store) resolveSecrets(book *SecretBook) error {
	var errs []error
	for i := range book.Secrets {
		sc := &book.Secrets[i]
		if sc.Slot == "" {
			continue
		}
		if s.Credentials == nil {
			errs = append(errs, ErrSecretCredentials)
			continue
		}
		value, err := s.Credentials.Read(sc.Slot)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		sc.Value = value
	}
	return errors.Join(errs...)
}

func (s Store) storeSecretValues(book *SecretBook, original map[string]string) error {
	for i := range book.Secrets {
		sc := &book.Secrets[i]
		if sc.Slot == "" || original[sc.Slot] != sc.Value {
			if s.Credentials == nil {
				return ErrSecretCredentials
			}
			slot, err := s.Credentials.Save(sc.Value)
			if err != nil {
				return err
			}
			sc.Slot = slot
		}
		sc.Value = ""
	}
	return nil
}
