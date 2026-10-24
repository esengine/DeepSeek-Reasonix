package workspacelease

import "slices"

// AddWriteAdmission retains each runtime's resource domain until the shared
// owner ends. Rebuilds cannot remove barriers still owed to background writers.
func (o *Owner) AddWriteAdmission(key string, check func([]string) error) {
	if o == nil || check == nil {
		return
	}
	o.admissionMu.Lock()
	defer o.admissionMu.Unlock()
	if o.admissions == nil {
		o.admissions = map[string]func([]string) error{}
	}
	if _, exists := o.admissions[key]; !exists {
		o.admissions[key] = check
	}
}

func (o *Owner) checkAdmission(paths []string) error {
	o.admissionMu.RLock()
	keys := make([]string, 0, len(o.admissions))
	for key := range o.admissions {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	checks := make([]func([]string) error, 0, len(keys))
	for _, key := range keys {
		checks = append(checks, o.admissions[key])
	}
	o.admissionMu.RUnlock()
	for _, check := range checks {
		if err := check(paths); err != nil {
			return err
		}
	}
	return nil
}
