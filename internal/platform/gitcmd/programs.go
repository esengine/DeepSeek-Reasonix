package gitcmd

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// programKeys are the configuration keys through which a git read runs a
// program: change detection, content conversion, external diff, signature
// checks. Listing them is how a caller learns a read is not only a read.
const programKeys = `^(core\.fsmonitor|diff\.external|diff\..+\.(textconv|command)|filter\..+\.(clean|smudge|process)|gpg\.program|gpg\..+\.program)$`

// RepositoryPrograms lists the program-running keys defined at repository
// scope — the config its author controls, includes reported under the scope of
// the file that includes them — for the repository at dir. The user's own
// global and system settings are not listed.
func RepositoryPrograms(ctx context.Context, dir string) ([]string, error) {
	query := []string{"config", "--show-scope", "--name-only", "-z", "--get-regexp", programKeys}
	out, err := listConfig(ctx, dir, nil, nil, query)
	// 1: no such key. 128: not a repository, or a config git itself refuses, in
	// which case a read fails before it can run anything.
	if code := exitCode(err); code == 1 || code == 128 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRepositoryDrivers, err)
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	var keys []string
	for i := 0; i+1 < len(fields); i += 2 {
		if slices.Contains(repositoryScopes, fields[i]) && !slices.Contains(keys, fields[i+1]) {
			keys = append(keys, fields[i+1])
		}
	}
	return keys, nil
}
