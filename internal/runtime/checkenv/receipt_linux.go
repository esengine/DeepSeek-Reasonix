//go:build linux

package checkenv

func (i *Invocation) recordReceipt() {
	input := i.state.input
	input.EnvironmentReceipt = digest(struct{ Identity, Execution, Call, Runtime, Source, Capture string }{
		i.identity, i.executionID, i.callID, i.state.runtimeDigest, i.state.sourceDigest, i.spec.captureIdentity()})
	recordOwnedReceipt(input, i.executionID, i.spec.captureIdentity())
	i.state.input = input
}
