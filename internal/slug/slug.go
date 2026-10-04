// Package slug turns Russian painting titles into URL-safe Latin slugs.
package slug

import "strings"

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh",
	'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu",
	'я': "ya",
}

const maxLen = 80

// Make returns a lowercase slug made of Latin letters, digits and single dashes.
// It returns "" when the title contains no letters or digits.
func Make(title string) string {
	var b strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(title) {
		var part string
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			part = string(r)
		} else if t, ok := translit[r]; ok {
			part = t
		} else {
			pendingDash = b.Len() > 0
			continue
		}
		if part == "" {
			continue
		}
		if pendingDash {
			b.WriteByte('-')
			pendingDash = false
		}
		b.WriteString(part)
	}
	s := b.String()
	if len(s) > maxLen {
		s = strings.TrimRight(s[:maxLen], "-")
	}
	return s
}
