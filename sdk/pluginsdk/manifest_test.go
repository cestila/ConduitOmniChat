package pluginsdk

import (
	"strings"
	"testing"

	"github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

func validManifest() *conduitv1.PluginManifest {
	return &conduitv1.PluginManifest{
		Id:              "telegram",
		DisplayName:     "Telegram",
		Provider:        conduitv1.Provider_PROVIDER_TELEGRAM,
		ProtocolVersion: ProtocolVersion,
	}
}

func TestValidateManifestAcceptsValid(t *testing.T) {
	if err := ValidateManifest(validManifest()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifestRejects(t *testing.T) {
	cases := []struct {
		name string
		edit func(*conduitv1.PluginManifest)
		want string
	}{
		{"nil", nil, "manifest is nil"},
		{"empty id", func(m *conduitv1.PluginManifest) { m.Id = "" }, "invalid plugin id"},
		{"uppercase id", func(m *conduitv1.PluginManifest) { m.Id = "Telegram" }, "invalid plugin id"},
		{"path id", func(m *conduitv1.PluginManifest) { m.Id = "../x" }, "invalid plugin id"},
		{"no name", func(m *conduitv1.PluginManifest) { m.DisplayName = "" }, "display_name"},
		{"no provider", func(m *conduitv1.PluginManifest) { m.Provider = conduitv1.Provider_PROVIDER_UNSPECIFIED }, "provider"},
		{"protocol", func(m *conduitv1.PluginManifest) { m.ProtocolVersion = 99 }, "protocol version 99"},
		{"dup field", func(m *conduitv1.PluginManifest) {
			m.CredentialFields = []*conduitv1.CredentialField{{Key: "a", Label: "A"}, {Key: "a", Label: "B"}}
		}, "duplicate credential field"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m *conduitv1.PluginManifest
			if tc.edit != nil {
				m = validManifest()
				tc.edit(m)
			}
			err := ValidateManifest(m)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateManifest() = %v, want error containing %q", err, tc.want)
			}
		})
	}
}
