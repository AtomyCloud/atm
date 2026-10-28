package runner

import (
	"context"
	"strings"
	"testing"
)

func TestInterruptedDoesNotPromisePrivilegedProcessCleanup(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		out := interrupted("id", err)
		if !strings.Contains(out.Message, "Privileged or detached work and partial changes may remain") || strings.Contains(out.Message, "process tree terminated") {
			t.Fatal(out)
		}
		if err == context.Canceled && out.ExitCode != 130 || err == context.DeadlineExceeded && out.ExitCode != 124 {
			t.Fatal(out)
		}
	}
}
