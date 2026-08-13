package tamsinrelease

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/livewyer-ops/tamsin/ingestevent"
)

func TestPublishedImageProducesCompatibleExactDryRuns(t *testing.T) {
	eventDirectory := os.Getenv("TAMOSS_TAMSIN_EVENT_DIR")
	if eventDirectory == "" {
		t.Skip("TAMOSS_TAMSIN_EVENT_DIR is set by task test:tamsin:release")
	}
	for _, profile := range []string{"preserve", "demux", "muxed-segments", "essence-segments", "mpegts-segments"} {
		t.Run(profile, func(t *testing.T) {
			stream, err := os.Open(filepath.Join(eventDirectory, profile+".ndjson"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stream.Close() }()

			state, err := ingestevent.Reduce(stream)
			if err != nil {
				t.Fatalf("reduce published TAMSin stream: %v", err)
			}
			if state.Hello == nil || state.Hello.ToolVersion != "1.0.0-rc.3" || state.ProtocolVersion != "2.1" {
				t.Fatalf("release identity = protocol %q hello %#v", state.ProtocolVersion, state.Hello)
			}
			if state.Started == nil || state.Started.Profile != profile || state.Started.ProfileVersion != "1" ||
				state.Started.DryRunMode != "exact" || state.Started.VerificationMode != "none" {
				t.Fatalf("resolved treatment = %#v", state.Started)
			}
			if state.Finished == nil || state.Finished.Outcome != ingestevent.RunSucceeded || state.Finished.ExitCode != 0 {
				t.Fatalf("terminal result = %#v", state.Finished)
			}
			if len(state.Inputs) != 1 {
				t.Fatalf("planned inputs = %#v", state.Inputs)
			}
			input := state.Inputs[0]
			if input == nil || input.Finished == nil ||
				input.Finished.Status != ingestevent.InputPlanned || input.Finished.Profile != profile {
				t.Fatalf("planned input = %#v", input)
			}
			if input.Finished.SHA256 != "0f5e0d9d843ea3ca19f77bc543ea9bd8d6ef47ec1640fa8faf16354f0621b52b" {
				t.Fatalf("input digest = %q", input.Finished.SHA256)
			}
		})
	}
}
