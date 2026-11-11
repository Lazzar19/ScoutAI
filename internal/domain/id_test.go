package domain

import (
	"regexp"
	"testing"
)

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewIDIsUUIDVersion4(t *testing.T) {
	first := NewID()
	second := NewID()
	if !uuidV4.MatchString(first) {
		t.Fatalf("NewID() = %q, want a UUID version 4", first)
	}
	if first == second {
		t.Fatal("NewID() returned the same value twice")
	}
}
