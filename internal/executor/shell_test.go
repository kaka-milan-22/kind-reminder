package executor

import (
	"strings"
	"testing"
)

func TestCappedBufferTruncates(t *testing.T) {
	c := &cappedBuffer{max: 10}

	// Single write exceeding the cap is clipped, reports full length.
	n, err := c.Write([]byte("0123456789ABCDEF"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 16 {
		t.Fatalf("Write returned %d, want 16 (must report full length)", n)
	}
	if !c.truncated {
		t.Fatal("expected truncated=true")
	}

	// Further writes past the cap stay dropped but still report full length.
	if n, _ := c.Write([]byte("more")); n != 4 {
		t.Fatalf("Write returned %d, want 4", n)
	}

	got := c.String()
	if !strings.HasPrefix(got, "0123456789") {
		t.Fatalf("retained bytes = %q, want prefix %q", got, "0123456789")
	}
	if !strings.HasSuffix(got, "[output truncated]") {
		t.Fatalf("String() = %q, want truncation marker suffix", got)
	}
}

func TestCappedBufferUnderCap(t *testing.T) {
	c := &cappedBuffer{max: 64}
	c.Write([]byte("hello"))
	if c.truncated {
		t.Fatal("did not expect truncation")
	}
	if got := c.String(); got != "hello" {
		t.Fatalf("String() = %q, want %q", got, "hello")
	}
}
