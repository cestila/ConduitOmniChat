package pluginsdk

import (
	"fmt"
	"regexp"

	"github.com/cestila/ConduitOmniChat/sdk/conduitv1"
)

var pluginIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// ValidID reports whether id is a valid plugin id (lowercase, digits, dashes).
func ValidID(id string) bool { return pluginIDPattern.MatchString(id) }

// ValidateManifest checks the fields conduitd relies on before registering a plugin.
func ValidateManifest(m *conduitv1.PluginManifest) error {
	if m == nil {
		return fmt.Errorf("manifest is nil")
	}
	if !ValidID(m.GetId()) {
		return fmt.Errorf("invalid plugin id %q: use lowercase letters, digits and dashes", m.GetId())
	}
	if m.GetDisplayName() == "" {
		return fmt.Errorf("plugin %s: display_name is required", m.GetId())
	}
	if m.GetProvider() == conduitv1.Provider_PROVIDER_UNSPECIFIED {
		return fmt.Errorf("plugin %s: provider is required", m.GetId())
	}
	if m.GetProtocolVersion() != ProtocolVersion {
		return fmt.Errorf("plugin %s: protocol version %d, daemon speaks %d", m.GetId(), m.GetProtocolVersion(), ProtocolVersion)
	}
	seen := map[string]bool{}
	for _, f := range m.GetCredentialFields() {
		if f.GetKey() == "" {
			return fmt.Errorf("plugin %s: credential field without key", m.GetId())
		}
		if seen[f.GetKey()] {
			return fmt.Errorf("plugin %s: duplicate credential field %q", m.GetId(), f.GetKey())
		}
		seen[f.GetKey()] = true
	}
	return nil
}
