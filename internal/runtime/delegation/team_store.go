package delegation

import "fmt"

func (s *SubagentStore) teamMembers(parent, root, name string) ([]RunInfo, *RunInfo, error) {
	runs, err := s.Runs(parent, root)
	if err != nil {
		return nil, nil, err
	}
	members := []RunInfo{}
	var selected *RunInfo
	for _, run := range runs {
		if run.Kind != "member" {
			continue
		}
		members = append(members, run)
		if run.Name == name {
			if selected != nil {
				return nil, nil, fmt.Errorf("ambiguous member name; inspect the subagent directory")
			}
			copy := run
			selected = &copy
		}
	}
	return members, selected, nil
}

func (s *SubagentStore) retireMember(ref string) error {
	release, err := s.lock(ref)
	if err != nil {
		return err
	}
	defer release()
	meta, err := s.LoadMeta(ref)
	if err != nil {
		return err
	}
	meta.Kind = "retired-member"
	return s.saveMeta(meta)
}
