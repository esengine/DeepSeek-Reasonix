package teamroster

import "context"

// SetAgentPhoto serializes upload, publication and pruning across processes.
// Nil data clears the photo; an upload is owned until its roster write settles.
func (s Store) SetAgentPhoto(ctx context.Context, agent string, data []byte) error {
	return s.withLock(ctx, s.Dir, AvatarDir, func() error {
		var name string
		if data != nil {
			var err error
			name, err = s.SaveAvatar(data)
			if err != nil {
				return err
			}
		}
		r, err := s.Update(ctx, func(r *Roster) error {
			a, ok := r.Agent(agent)
			if !ok {
				return ErrUnknownAgent
			}
			a.Avatar.Image = name
			return nil
		})
		if err != nil {
			return err
		}
		return s.PruneAvatars(r)
	})
}
