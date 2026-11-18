package buildinfo

import "testing"

func TestSummaryDefaultsAndInjected(t *testing.T) {
	origVersion, origCommit, origDate := Version, Commit, Date
	t.Cleanup(func() {
		Version, Commit, Date = origVersion, origCommit, origDate
	})

	if Version != "dev" || Commit != "none" || Date != "unknown" {
		t.Fatalf("unexpected default values: Version=%q Commit=%q Date=%q", Version, Commit, Date)
	}
	if got := Summary(); got == "" {
		t.Fatal("expected non-empty summary")
	}

	cases := []struct {
		name    string
		version string
		commit  string
		date    string
		want    string
	}{
		{
			name:    "defaults",
			version: "dev",
			commit:  "none",
			date:    "unknown",
			want:    "dev",
		},
		{
			name:    "full release metadata",
			version: "v0.1.2",
			commit:  "abc1234",
			date:    "2026-11-18",
			want:    "v0.1.2 (commit abc1234, built 2026-11-18)",
		},
		{
			name:    "version only",
			version: "v0.1.2-dev",
			commit:  "none",
			date:    "unknown",
			want:    "v0.1.2-dev",
		},
		{
			name:    "version and commit",
			version: "v0.1.2",
			commit:  "abc1234",
			date:    "unknown",
			want:    "v0.1.2 (commit abc1234)",
		},
		{
			name:    "version and date",
			version: "v0.1.2",
			commit:  "none",
			date:    "2026-11-18",
			want:    "v0.1.2 (built 2026-11-18)",
		},
		{
			name:    "empty fields fallback to dev",
			version: "",
			commit:  "",
			date:    "",
			want:    "dev",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			Version, Commit, Date = tc.version, tc.commit, tc.date
			if got := Summary(); got != tc.want {
				t.Fatalf("Summary() = %q, want %q", got, tc.want)
			}
		})
	}
}
