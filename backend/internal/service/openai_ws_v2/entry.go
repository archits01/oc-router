package openai_ws_v2

import "context"

// EntryInput
type EntryInput struct {
	Ctx                context.Context
	ClientConn         FrameConn
	UpstreamConn       FrameConn
	FirstClientMessage []byte
	Options            RelayOptions
}

// RunEntry
func RunEntry(input EntryInput) (RelayResult, *RelayExit) {
	return runCaddyStyleRelay(
		input.Ctx,
		input.ClientConn,
		input.UpstreamConn,
		input.FirstClientMessage,
		input.Options,
	)
}
