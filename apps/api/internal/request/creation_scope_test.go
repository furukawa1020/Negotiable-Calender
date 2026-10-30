package request

import (
	"errors"
	"testing"
)

func TestCreationIDRequiresIdentityScope(t *testing.T) {
	key := "1234567890123456"
	for _, scope := range [][2]string{{"", "actor"}, {"org", ""}, {" \t\n", "actor"}, {"org", "　"}, {"", ""}} {
		id, err := CreationID(scope[0], scope[1], key)
		if id != "" || !errors.Is(err, ErrCreationForbidden) {
			t.Fatalf("empty scope accepted: %q %v", id, err)
		}
	}
	a, err := CreationID("org", "actor", key)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := CreationID("org", "actor", key)
	c, _ := CreationID("org2", "actor", key)
	d, _ := CreationID("org", "actor2", key)
	if a != b || a == c || a == d {
		t.Fatal("scope stability broken")
	}
}
