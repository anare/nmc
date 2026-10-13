package shell

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
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

func TestInteractiveSessionRunsCommandsThroughPseudoTerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY sessions are Unix-only")
	}
	session, err := StartSession(context.Background(), "/bin/sh", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Write([]byte("printf 'session-ready\\n'\nexit\n")); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- session.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shell session did not exit after receiving commands")
	}
	output := session.output.String()
	if !strings.Contains(output, "session-ready") {
		t.Fatalf("pty output = %q", output)
	}
}
