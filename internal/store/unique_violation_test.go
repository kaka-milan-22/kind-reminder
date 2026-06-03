package store

import (
	"errors"
	"testing"
)

func TestIsUniqueViolation(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unique", errors.New("UNIQUE constraint failed: executions.id"), true},
		{"unique lowercase", errors.New("unique constraint failed"), true},
		{"not null", errors.New("NOT NULL constraint failed: executions.status"), false},
		{"foreign key", errors.New("FOREIGN KEY constraint failed"), false},
		{"check", errors.New("CHECK constraint failed: jobs"), false},
		{"unrelated", errors.New("disk I/O error"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isUniqueViolation(c.err); got != c.want {
				t.Fatalf("isUniqueViolation(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
