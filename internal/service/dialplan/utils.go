// sentiric-dialplan-service/internal/service/dialplan/utils.go
package dialplan

import (
	"strings"
	"unicode"
)

func safeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toPtr(s string) *string {
	return &s
}

// extractUserPart, SIP URI/AOR'dan kullanıcı bölümünü güvenli bir şekilde ayıklar.
func extractUserPart(raw string) string {
	if raw == "anonymous" {
		return raw
	}
	s := raw
	if start := strings.Index(s, "<"); start != -1 {
		s = s[start+1:]
	}
	if end := strings.Index(s, ">"); end != -1 {
		s = s[:end]
	}
	if strings.HasPrefix(s, "sip:") {
		s = s[4:]
	} else if strings.HasPrefix(s, "sips:") {
		s = s[5:]
	}
	if atIndex := strings.Index(s, "@"); atIndex != -1 {
		s = s[:atIndex]
	} else if semiIndex := strings.Index(s, ";"); semiIndex != -1 {
		s = s[:semiIndex]
	}

	var sb strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// normalizePhoneNumber, telefon numarasını standart E.164 (90...) formatına getirir.
func normalizePhoneNumber(phone string) string {
	if phone == "anonymous" {
		return phone
	}
	var sb strings.Builder
	for _, ch := range phone {
		if unicode.IsDigit(ch) {
			sb.WriteRune(ch)
		}
	}
	cleaned := sb.String()

	if cleaned == "" {
		return phone
	}
	if len(cleaned) == 12 && strings.HasPrefix(cleaned, "90") {
		return cleaned
	}
	if len(cleaned) == 11 && strings.HasPrefix(cleaned, "0") {
		return "90" + cleaned[1:]
	}
	if len(cleaned) == 10 {
		return "90" + cleaned
	}
	return cleaned
}
