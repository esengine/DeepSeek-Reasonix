package delegation

import "context"

func (t *TaskTool) WithExecutionPresence(enter func(context.Context, string, bool) (func(), error)) *TaskTool {
	t.executionPresence = enter
	return t
}

func (p *profileExecution) enterExecutionPresence(ctx context.Context) error {
	if p.task.executionPresence == nil {
		return nil
	}
	release, err := p.task.executionPresence(ctx, p.spec.Context.parentSession(ctx), !p.spec.Grant.ReadOnly)
	if err != nil {
		return err
	}
	p.presenceRelease = release
	return nil
}
