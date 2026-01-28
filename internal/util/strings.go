package util

import (
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// ToValidUTF8 ensures a string is valid UTF-8.
// If the string contains invalid UTF-8 sequences, it attempts to decode
// as Latin-1 (ISO-8859-1), which is a common encoding in older git repos.
// This preserves characters like ä, ö, ü, é, etc. instead of replacing them.
func ToValidUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}

	// Try decoding as Latin-1 (covers most Western European legacy encodings)
	decoded, err := charmap.ISO8859_1.NewDecoder().String(s)
	if err == nil {
		return decoded
	}

	// Fallback: decode byte-by-byte as Latin-1
	// This always works since Latin-1 maps 1:1 to Unicode codepoints 0-255
	runes := make([]rune, len(s))
	for i := 0; i < len(s); i++ {
		runes[i] = rune(s[i])
	}
	return string(runes)
}

// ToValidUTF8Bytes ensures bytes represent valid UTF-8.
func ToValidUTF8Bytes(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	return []byte(ToValidUTF8(string(b)))
}
