package sandbox

// backendWriteDirs is empty on Windows, where no backend confines writes.
func backendWriteDirs(Spec) []string { return nil }
