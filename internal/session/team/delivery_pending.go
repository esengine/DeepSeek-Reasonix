package team

// MessagePending checks durable work before an inbox resumes it. A restart
// settles exchanges before opening seats; an interrupted message cannot run.
func (s *Service) MessagePending(seat SeatRef, exchange string) (bool, error) {
	book, err := s.store.Exchanges()
	if err != nil {
		return false, err
	}
	x, ok := find(book, exchange)
	return ok && x.To == seat.Agent && !x.State.Settled(), nil
}

// GroupTurnPending waits for an in-progress delivery to publish its round, then
// refuses inbox work for a step that recovery or the group already cut.
func (s *Service) GroupTurnPending(seat SeatRef, step string) (bool, error) {
	defer s.rounds.lock(seat.Group)()
	round, err := s.store.Round(seat.Group)
	if err != nil {
		return false, err
	}
	return round.Speaking != nil && round.Speaking.Member == seat.Agent && round.Speaking.Key == step, nil
}
