package conduitv1

import (
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// typedStringField returns the descriptor of a string field of
// ProviderCredentials named key, or nil when key is a custom value.
func typedStringField(c *ProviderCredentials, key string) protoreflect.FieldDescriptor {
	fd := c.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(key))
	if fd == nil || fd.Kind() != protoreflect.StringKind || fd.IsList() || fd.IsMap() {
		return nil
	}
	return fd
}

// Value returns the credential stored under key. Values carried in the
// values map win over the typed field of the same name.
func (c *ProviderCredentials) Value(key string) string {
	if c == nil {
		return ""
	}
	if v, ok := c.GetValues()[key]; ok {
		return v
	}
	if fd := typedStringField(c, key); fd != nil {
		return c.ProtoReflect().Get(fd).String()
	}
	return ""
}

// NormalizeCredentials moves values whose key names a typed field into that
// field (trimmed), so plugins and stored accounts keep using typed fields.
// Blank values never overwrite a typed field. Custom keys stay in the values map.
func NormalizeCredentials(c *ProviderCredentials) {
	if c == nil {
		return
	}
	for key, v := range c.GetValues() {
		fd := typedStringField(c, key)
		if fd == nil {
			continue
		}
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			c.ProtoReflect().Set(fd, protoreflect.ValueOfString(trimmed))
		}
		delete(c.Values, key)
	}
	if len(c.Values) == 0 {
		c.Values = nil
	}
}
