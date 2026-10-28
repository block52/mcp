package chain

import "testing"

func TestOwnerDefaultsToOwn(t *testing.T) {
	c, _ := New(Config{}) // no Owner set
	if got := c.Owner(); got != OwnerOwn {
		t.Fatalf("default owner = %q, want %q", got, OwnerOwn)
	}
}

func TestOwnerDelegatedPreserved(t *testing.T) {
	c, _ := New(Config{Owner: OwnerDelegated})
	if got := c.Owner(); got != OwnerDelegated {
		t.Fatalf("owner = %q, want %q", got, OwnerDelegated)
	}
}

func TestOwnerUnknownNormalizesToOwn(t *testing.T) {
	// Any value other than the explicit "delegated" is treated as own — we never
	// silently claim a key is delegated, and never fail closed into read-only.
	c, _ := New(Config{Owner: "garbage"})
	if got := c.Owner(); got != OwnerOwn {
		t.Fatalf("owner = %q, want %q", got, OwnerOwn)
	}
}
