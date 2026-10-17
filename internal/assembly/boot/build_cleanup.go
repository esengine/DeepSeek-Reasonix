package boot

func (b *builder) discardBuildResources() {
	if b.cleanup != nil {
		b.cleanup()
	}
	if b.tools.browser != nil && b.opts.BrowserSession == nil {
		b.tools.browser.Release()
	}
	if b.tools.env.sessionTemp != nil && b.opts.SessionTemp == nil {
		b.tools.env.sessionTemp.Release()
	}
}
