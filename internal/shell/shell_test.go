package shell

import (
	"context"
	"testing"
)

func TestRunCapturesCombinedOutputAndExitCode(t *testing.T) {
	output, err := Run(context.Background(), "/bin/sh", t.TempDir(), "printf out; printf err >&2; exit 7")
	if string(output) != "outerr" || ExitCode(err) != 7 {
		t.Fatalf("Run returned output=%q exit=%d", output, ExitCode(err))
	}
}

func TestBufferRetainsOnlyItsConfiguredTail(t *testing.T) {
	buffer := NewBuffer(5)
	_, _ = buffer.Write([]byte("123"))
	_, _ = buffer.Write([]byte("456"))
	if got := buffer.String(); got != "23456" {
		t.Fatalf("buffer = %q, want %q", got, "23456")
	}
}
