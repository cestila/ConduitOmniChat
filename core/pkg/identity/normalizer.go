package identity

import (
	"regexp"
	"strings"
)

var (
	nonDigitRegex = regexp.MustCompile(`\D`)
	emailRegex    = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
)

// NormalizeIdentifier normaliza un número de teléfono a E.164 o un email a minúsculas.
// Preserva intactos los identificadores de grupos (ej: @g.us en WhatsApp).
func NormalizeIdentifier(input string) string {
	cleaned := strings.TrimSpace(input)

	// Si es un identificador de grupo o identificador de dispositivo/usuario privado de WhatsApp (@lid)
	if strings.HasSuffix(cleaned, "@g.us") || strings.HasSuffix(cleaned, "@broadcast") || strings.HasSuffix(cleaned, "@lid") {
		return strings.ToLower(cleaned)
	}

	if IsEmail(cleaned) {
		return strings.ToLower(cleaned)
	}

	// Si contiene dígitos, normalizamos a formato numérico estilo E.164
	digitsOnly := nonDigitRegex.ReplaceAllString(cleaned, "")
	if len(digitsOnly) > 0 {
		return "+" + digitsOnly
	}

	// Para handles de telegram o discord (ej. @usuario)
	return strings.ToLower(cleaned)
}

// IsEmail verifica si un string es un formato válido de correo electrónico.
func IsEmail(input string) bool {
	return emailRegex.MatchString(strings.TrimSpace(input))
}

// IsIdentityEmail distingue un email real de persona de un JID interno de WhatsApp
// (ej. 5491112345678@s.whatsapp.net), que también satisface la forma de un email.
func IsIdentityEmail(input string) bool {
	if !IsEmail(input) {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(input))
	for _, suffix := range []string{"@s.whatsapp.net", "@c.us", "@g.us", "@lid", "@broadcast", "@newsletter"} {
		if strings.HasSuffix(lower, suffix) {
			return false
		}
	}
	return true
}

// NormalizeEmail devuelve el email canónico en minúsculas, o "" si no es una identidad de email válida.
func NormalizeEmail(input string) string {
	if !IsIdentityEmail(input) {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(input))
}

// PhoneFromIdentifier extrae un teléfono canónico E.164 de un identificador de proveedor.
// Reconoce números con prefijo internacional y JIDs de WhatsApp (@s.whatsapp.net).
// Devuelve "" para identificadores que no son teléfonos (IDs de Telegram/Discord, grupos, @lid, emails).
func PhoneFromIdentifier(input string) string {
	cleaned := strings.TrimSpace(input)
	if cleaned == "" {
		return ""
	}

	lower := strings.ToLower(cleaned)
	if strings.HasSuffix(lower, "@s.whatsapp.net") {
		userPart := cleaned[:strings.Index(cleaned, "@")]
		return canonicalPhoneDigits(userPart)
	}

	// Cualquier otro JID o email no representa un teléfono
	if strings.Contains(cleaned, "@") {
		return ""
	}

	if strings.HasPrefix(cleaned, "00") {
		return canonicalPhoneDigits(cleaned[2:])
	}

	// Solo aceptamos números explícitamente internacionales para no confundir
	// IDs numéricos de Telegram o snowflakes de Discord con teléfonos.
	if !strings.HasPrefix(cleaned, "+") {
		return ""
	}
	return canonicalPhoneDigits(cleaned)
}

// canonicalPhoneDigits normaliza la parte numérica a E.164 si la longitud es plausible.
func canonicalPhoneDigits(input string) string {
	digits := nonDigitRegex.ReplaceAllString(input, "")
	if len(digits) < 8 || len(digits) > 15 {
		return ""
	}
	return "+" + digits
}
