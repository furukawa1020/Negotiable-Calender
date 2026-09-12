package main

import "testing"

func TestCleanupRequiresExplicitValidTarget(t *testing.T) {
	for _, args := range [][]string{nil, {"-project", "demo-test"}, {"-user", "alice"}, {"-project", "demo-test", "-user", "a/b"}, {"-project", "demo-test", "-user", "alice", "-timeout", "0s"}} {
		if err := run(args); err == nil {
			t.Fatalf("accepted invalid target: %v", args)
		}
	}
}
