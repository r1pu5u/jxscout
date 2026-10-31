package main

import "testing"

func TestProjectOption(t *testing.T) {
	for _, args := range [][]string{{"--project", "foo"}, {"--project=foo"}, {"-p", "foo"}, {"-p=foo"}, {"--project-name", "foo"}, {"--project", "old", "-p", "foo"}} {
		rewritten := rewriteProjectAlias(append([]string{"jxscout"}, args...))
		got, found := projectOption(rewritten[1:])
		if !found || got != "foo" {
			t.Fatalf("%v => %q, %v", args, got, found)
		}
	}
	if _, found := projectOption([]string{"--port", "3333"}); found {
		t.Fatal("unexpected project")
	}
}
