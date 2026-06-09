package notifier

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateForTelegram(t *testing.T) {
	t.Run("short text passes through unchanged", func(t *testing.T) {
		in := "hello world"
		if got := truncateForTelegram(in); got != in {
			t.Fatalf("got %q, want unchanged %q", got, in)
		}
	})

	t.Run("text exactly at the limit is unchanged", func(t *testing.T) {
		in := strings.Repeat("a", telegramMaxChars)
		if got := truncateForTelegram(in); got != in {
			t.Fatalf("length %d at the limit should be unchanged, got length %d",
				telegramMaxChars, utf8.RuneCountInString(got))
		}
	})

	t.Run("over-limit text is truncated to the limit with a marker", func(t *testing.T) {
		in := strings.Repeat("a", telegramMaxChars*2)
		got := truncateForTelegram(in)
		if n := utf8.RuneCountInString(got); n > telegramMaxChars {
			t.Fatalf("truncated length %d exceeds limit %d", n, telegramMaxChars)
		}
		if !strings.Contains(got, "truncated") {
			t.Fatalf("expected a truncation marker, got tail %q", got[len(got)-40:])
		}
	})

	t.Run("does not split a multibyte rune", func(t *testing.T) {
		// All multibyte runes; a byte-based cut would produce invalid UTF-8.
		in := strings.Repeat("中", telegramMaxChars*2)
		got := truncateForTelegram(in)
		if !utf8.ValidString(got) {
			t.Fatal("truncation produced invalid UTF-8")
		}
		if n := utf8.RuneCountInString(got); n > telegramMaxChars {
			t.Fatalf("truncated length %d exceeds limit %d", n, telegramMaxChars)
		}
	})
}
