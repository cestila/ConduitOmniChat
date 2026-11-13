package conduitv1

import "testing"

func TestCredentialValuePrefersValuesMap(t *testing.T) {
	c := &ProviderCredentials{ApiHash: "typed", Values: map[string]string{"api_hash": "mapped", "custom": "x"}}
	if got := c.Value("api_hash"); got != "mapped" {
		t.Fatalf("Value(api_hash) = %q, want mapped", got)
	}
	if got := c.Value("custom"); got != "x" {
		t.Fatalf("Value(custom) = %q, want x", got)
	}
}

func TestCredentialValueFallsBackToTypedField(t *testing.T) {
	c := &ProviderCredentials{ApiId: "123", BotToken: "tok"}
	if got := c.Value("api_id"); got != "123" {
		t.Fatalf("Value(api_id) = %q, want 123", got)
	}
	if got := c.Value("bot_token"); got != "tok" {
		t.Fatalf("Value(bot_token) = %q, want tok", got)
	}
	if got := c.Value("missing"); got != "" {
		t.Fatalf("Value(missing) = %q, want empty", got)
	}
	var nilCreds *ProviderCredentials
	if got := nilCreds.Value("api_id"); got != "" {
		t.Fatalf("nil Value = %q, want empty", got)
	}
}

func TestNormalizeCredentialsCopiesKnownKeysIntoTypedFields(t *testing.T) {
	c := &ProviderCredentials{Phone: "+1000", Values: map[string]string{"api_id": " 42 ", "api_hash": "h", "custom": "keep"}}
	NormalizeCredentials(c)
	if c.ApiId != "42" || c.ApiHash != "h" {
		t.Fatalf("typed fields not filled: %+v", c)
	}
	if c.Phone != "+1000" {
		t.Fatalf("phone without value must stay, got %q", c.Phone)
	}
	if c.Values["custom"] != "keep" {
		t.Fatalf("custom value lost: %v", c.Values)
	}
	if _, ok := c.Values["api_id"]; ok {
		t.Fatalf("known key must move out of values: %v", c.Values)
	}
	NormalizeCredentials(nil) // must not panic
}

func TestNormalizeCredentialsEmptyValueDoesNotWipeTypedField(t *testing.T) {
	c := &ProviderCredentials{ApiHash: "stored", Values: map[string]string{"api_hash": "  "}}
	NormalizeCredentials(c)
	if c.ApiHash != "stored" {
		t.Fatalf("empty value wiped typed field: %q", c.ApiHash)
	}
	if len(c.Values) != 0 {
		t.Fatalf("known key must leave values: %v", c.Values)
	}
}
