package e2eharness

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// SanitizeCharName keeps only letters and ensures 2..12 length for 3.3.5 create.
// Digits are rejected by the server as CHAR_NAME_MIXED_LANGUAGES (0x5D).
func SanitizeCharName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) < 2 {
		out = "Bot" + out
	}
	if len(out) > 12 {
		out = out[:12]
	}
	// Title-case first letter for nicer names.
	runes := []rune(out)
	runes[0] = unicode.ToUpper(runes[0])
	for i := 1; i < len(runes); i++ {
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}

// UniqueLetterNames returns candidate pure-letter character names.
func UniqueLetterNames(preferred string, n int) []string {
	base := SanitizeCharName(preferred)
	// Encode time+index as base-26 letter suffixes for uniqueness.
	seed := time.Now().UnixNano()
	out := make([]string, 0, n+1)
	out = append(out, base)
	for i := 0; i < n; i++ {
		suffix := base26(uint64(seed)+uint64(i), 4)
		name := SanitizeCharName(fmt.Sprintf("%s%s", base[:min(4, len(base))], suffix))
		out = append(out, name)
	}
	// Static fallbacks last.
	out = append(out, "Petown", "Petsig", "Gblead", "Gbsign", "Gbalt", "Gbaltb", "Gbaltc", "Gbaltd")
	return out
}

// Base26 encodes v as lowercase letters (width digits).
func Base26(v uint64, width int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		b[i] = alphabet[v%26]
		v /= 26
	}
	return string(b)
}

func base26(v uint64, width int) string { return Base26(v, width) }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
