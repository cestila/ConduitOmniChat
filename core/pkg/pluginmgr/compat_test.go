package pluginmgr

import (
	"strings"
	"testing"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "v-prefix greater patch", a: "v0.1.2", b: "0.1.1", want: 1},
		{name: "v-prefix lesser patch", a: "0.1.1", b: "v0.1.2", want: -1},
		{name: "equal with and without v", a: "v0.1.2", b: "0.1.2", want: 0},
		{name: "numeric minor comparison", a: "1.10.0", b: "1.2.0", want: 1},
		{name: "major beats minor", a: "2.0.0", b: "1.99.99", want: 1},
		{name: "build metadata ignored", a: "0.1.2+build.1", b: "0.1.2+build.99", want: 0},
		{name: "prerelease lower than release", a: "1.0.0-alpha", b: "1.0.0", want: -1},
		{name: "release higher than prerelease", a: "1.0.0", b: "1.0.0-rc.1", want: 1},
		{name: "prerelease numeric vs lexical", a: "1.0.0-alpha.1", b: "1.0.0-alpha.beta", want: -1},
		{name: "prerelease numeric comparison", a: "1.0.0-rc.10", b: "1.0.0-rc.2", want: 1},
		{name: "prerelease field count", a: "1.0.0-alpha", b: "1.0.0-alpha.1", want: -1},
		{name: "missing patch treated as zero", a: "1.2", b: "1.2.0", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareSemver(tt.a, tt.b)
			if got != tt.want {
				t.Fatalf("CompareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCheckCompatibility(t *testing.T) {
	tests := []struct {
		name              string
		entry             RegistryPlugin
		coreVersion       string
		protoVersion      uint32
		installedVersions map[string]string
		wantOK            bool
		wantReasonSubstr  string
	}{
		{
			name: "all constraints satisfied",
			entry: RegistryPlugin{
				ID:                 "whatsapp",
				MinProtocolVersion: 1,
				MaxProtocolVersion: 2,
				MinCoreVersion:     "0.1.0",
				Dependencies: []PluginDependency{
					{PluginID: "media-helper", MinVersion: "0.1.0"},
				},
			},
			coreVersion:       "0.1.2",
			protoVersion:      1,
			installedVersions: map[string]string{"media-helper": "0.1.5"},
			wantOK:            true,
		},
		{
			name: "protocol below minimum",
			entry: RegistryPlugin{
				ID:                 "whatsapp",
				MinProtocolVersion: 2,
				MaxProtocolVersion: 3,
			},
			coreVersion:      "0.2.0",
			protoVersion:     1,
			wantOK:           false,
			wantReasonSubstr: "protocol",
		},
		{
			name: "protocol above maximum",
			entry: RegistryPlugin{
				ID:                 "whatsapp",
				MinProtocolVersion: 1,
				MaxProtocolVersion: 1,
			},
			coreVersion:      "0.2.0",
			protoVersion:     2,
			wantOK:           false,
			wantReasonSubstr: "protocol",
		},
		{
			name: "core version below minimum",
			entry: RegistryPlugin{
				ID:                 "whatsapp",
				MinProtocolVersion: 1,
				MaxProtocolVersion: 1,
				MinCoreVersion:     "0.2.0",
			},
			coreVersion:      "0.1.2",
			protoVersion:     1,
			wantOK:           false,
			wantReasonSubstr: "0.2.0",
		},
		{
			name: "skips min_core_version when core is dev",
			entry: RegistryPlugin{
				ID:                 "whatsapp",
				MinProtocolVersion: 1,
				MaxProtocolVersion: 1,
				MinCoreVersion:     "0.2.0",
			},
			coreVersion:  "dev",
			protoVersion: 1,
			wantOK:       true,
		},
		{
			name: "missing required plugin dependency",
			entry: RegistryPlugin{
				ID:                 "custom",
				MinProtocolVersion: 1,
				MaxProtocolVersion: 1,
				Dependencies: []PluginDependency{
					{PluginID: "whatsapp", MinVersion: "0.1.0"},
				},
			},
			coreVersion:       "0.1.2",
			protoVersion:      1,
			installedVersions: map[string]string{},
			wantOK:            false,
			wantReasonSubstr:  "whatsapp",
		},
		{
			name: "dependency below minimum version",
			entry: RegistryPlugin{
				ID:                 "custom",
				MinProtocolVersion: 1,
				MaxProtocolVersion: 1,
				Dependencies: []PluginDependency{
					{PluginID: "whatsapp", MinVersion: "0.2.0"},
				},
			},
			coreVersion:       "0.2.0",
			protoVersion:      1,
			installedVersions: map[string]string{"whatsapp": "0.1.2"},
			wantOK:            false,
			wantReasonSubstr:  "whatsapp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := CheckCompatibility(tt.entry, tt.coreVersion, tt.protoVersion, tt.installedVersions)
			if ok != tt.wantOK {
				t.Fatalf("CheckCompatibility() ok = %v (reason=%q), want %v", ok, reason, tt.wantOK)
			}
			if !tt.wantOK && tt.wantReasonSubstr != "" && !strings.Contains(strings.ToLower(reason), strings.ToLower(tt.wantReasonSubstr)) {
				t.Fatalf("reason %q does not contain %q", reason, tt.wantReasonSubstr)
			}
			if tt.wantOK && reason != "" {
				t.Fatalf("expected empty reason when compatible, got %q", reason)
			}
		})
	}
}
