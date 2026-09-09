package agent

import (
	"fmt"
	"sort"
)

// SetRecoveryPreferred records exactly one explicit preferred lineage member.
// Clearing old choices first can only fall back to an unresolved group; an
// interrupted update can never leave two canonical choices.
func SetRecoveryPreferred(paths []string, chosenPath string) error {
	chosenPath = canonicalSessionSavePath(chosenPath)
	if chosenPath == "" {
		return fmt.Errorf("empty preferred recovery path")
	}
	unique := map[string]struct{}{}
	for _, path := range paths {
		if path = canonicalSessionSavePath(path); path != "" {
			unique[path] = struct{}{}
		}
	}
	if _, ok := unique[chosenPath]; !ok {
		return fmt.Errorf("preferred recovery path is outside the lineage")
	}
	ordered, err := validatedRecoveryPreferenceMembers(unique)
	if err != nil {
		return err
	}
	for _, path := range ordered {
		if err := UpdateBranchMeta(path, false, clearRecoveryPreference); err != nil {
			return err
		}
	}
	chosen, err := LoadSession(chosenPath)
	if err != nil || chosen == nil || chosen.normalizedDirty || chosen.eventLogDamaged {
		return fmt.Errorf("could not fingerprint preferred recovery branch")
	}
	digest, err := digestSessionMessages(chosen.Snapshot())
	if err != nil {
		return err
	}
	return UpdateBranchMeta(chosenPath, false, func(meta *BranchMeta) error {
		meta.RecoveryPreferred = true
		meta.RecoveryPreferredDigest = digestString(digest)
		return nil
	})
}

func validatedRecoveryPreferenceMembers(unique map[string]struct{}) ([]string, error) {
	ordered := make([]string, 0, len(unique))
	recoveredMembers, normalMembers := 0, 0
	for path := range unique {
		meta, ok, err := LoadBranchMeta(path)
		if err != nil || !ok {
			return nil, fmt.Errorf("invalid recovery lineage member")
		}
		if meta.Recovered {
			recoveredMembers++
		} else {
			normalMembers++
		}
		ordered = append(ordered, path)
	}
	if recoveredMembers == 0 || normalMembers > 1 {
		return nil, fmt.Errorf("invalid recovery lineage members")
	}
	sort.Strings(ordered)
	return ordered, nil
}

func clearRecoveryPreference(meta *BranchMeta) error {
	meta.RecoveryPreferred = false
	meta.RecoveryPreferredDigest = ""
	return nil
}
