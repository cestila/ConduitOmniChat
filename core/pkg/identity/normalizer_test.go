package identity

import "testing"

func TestNormalizePhoneAcceptsProviderDigitsWithoutPlus(t *testing.T) {
	if got := NormalizePhone("5491112345678"); got != "+5491112345678" {
		t.Fatalf("got %q, want +5491112345678", got)
	}
	if got := PhoneFromIdentifier("5491112345678"); got != "" {
		t.Fatalf("PhoneFromIdentifier must still reject raw non-+ IDs, got %q", got)
	}

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "e164 with plus", raw: "+5491112345678", want: "+5491112345678"},
		{name: "formatted international", raw: "+54 9 11 1234-5678", want: "+5491112345678"},
		{name: "00 prefix", raw: "005491112345678", want: "+5491112345678"},
		{name: "whatsapp jid", raw: "5491112345678@s.whatsapp.net", want: "+5491112345678"},
		{name: "too short", raw: "1234567", want: ""},
		{name: "too long", raw: "1234567890123456", want: ""},
		{name: "email", raw: "user@example.com", want: ""},
		{name: "empty", raw: "   ", want: ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizePhone(tt.raw); got != tt.want {
				t.Fatalf("NormalizePhone(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
