package version

import (
	"strings"
	"testing"
)

func TestInfoStartsWithStableMachineReadableLine(t *testing.T) {
	oldVersion, oldCommit, oldBuildDate := Version, Commit, BuildDate
	t.Cleanup(func() { Version, Commit, BuildDate = oldVersion, oldCommit, oldBuildDate })
	Version, Commit, BuildDate = "v1.2.3", "abcdef0", "2026-08-14T00:00:00Z"
	got := Info()
	if !strings.HasPrefix(got, "monitorkit v1.2.3\n") {
		t.Fatalf("Info() = %q", got)
	}
	for _, want := range []string{"commit: abcdef0", "build date: 2026-08-14T00:00:00Z", "platform:"} {
		if !strings.Contains(got, want) {
			t.Errorf("Info() does not contain %q", want)
		}
	}
}
