package cli

func (m *chatTUI) reflowTranscript(terminalWidth int) {
	m.ensureTranscriptSources()
	for i, source := range m.transcriptSources {
		if source.kind == transcriptSourceFixed && source.render == nil {
			continue
		}
		m.transcript[i] = m.renderTranscriptSource(source, terminalWidth)
	}
}

func (m *chatTUI) commitTranscriptSource(source transcriptSource) {
	rendered := m.renderTranscriptSource(source, m.width)
	*m.pendingCommit = append(*m.pendingCommit, rendered)
	m.appendTranscriptBlock(rendered, source)
}

func (m *chatTUI) commitRenderedLine(render func(int) string) {
	m.commitTranscriptSource(transcriptSource{render: render})
}

func (m *chatTUI) rewriteRenderedLine(index int, render func(int) string) {
	m.setTranscriptBlock(index, render(m.width), transcriptSource{render: render})
}
