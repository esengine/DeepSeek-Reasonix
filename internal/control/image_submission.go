package control

import "context"

func hasExplicitImageAttachment(line string) bool {
	for _, token := range parseRefTokens(line) {
		if isAttachmentRef(token) && isImageAttachmentRef(token) {
			return true
		}
	}
	return false
}

func (c *Controller) runPreparedRefTurn(
	input, refLine, display, original string,
	resolve func(context.Context, string) resolvedReferences,
	decorate func(context.Context) context.Context,
	admission turnAdmission,
) {
	if !hasExplicitImageAttachment(refLine) {
		c.runGuardedWithAdmission(func(ctx context.Context) error {
			return c.runRefTurnWithResolverSync(decorate(ctx), input, refLine, display, original, resolve)
		}, admission)
		return
	}
	// Freeze image bytes before admission so later file changes cannot alter the accepted turn.
	resolveCtx := admission.durableCtx
	if resolveCtx == nil {
		resolveCtx = c.attachmentContext()
	}
	resolveCtx = contextWithPreparedImageReferences(resolveCtx, admission.images)
	resolved := resolve(resolveCtx, refLine)
	if len(resolved.imageErrs) > 0 {
		err := ImageReferenceFailures(resolved.imageErrs)
		c.notice(err.Error())
		return
	}
	c.runGuardedWithAdmission(func(ctx context.Context) error {
		return c.runResolvedRefTurnSync(decorate(ctx), input, display, original, resolved)
	}, admission)
}
