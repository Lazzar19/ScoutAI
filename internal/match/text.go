package match

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func hasPhrase(text, phrase string) bool {
	text = fold(text)
	phrase = strings.TrimSpace(fold(phrase))
	if phrase == "" {
		return false
	}
	for start := 0; start < len(text); {
		rel := strings.Index(text[start:], phrase)
		if rel < 0 {
			return false
		}
		at := start + rel
		if leftBoundary(text, at) && rightBoundary(text, at+len(phrase)) {
			return true
		}
		next := at + len(phrase)
		if next <= start {
			return false
		}
		start = next
	}
	return false
}

func anyPhrase(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if hasPhrase(text, phrase) {
			return true
		}
	}
	return false
}

func countPhrases(text string, phrases []string) int {
	found := 0
	for _, phrase := range phrases {
		if hasPhrase(text, phrase) {
			found++
		}
	}
	return found
}

func leftBoundary(text string, at int) bool {
	if at <= 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(text[:at])
	return !isWord(r)
}

func rightBoundary(text string, at int) bool {
	if at >= len(text) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(text[at:])
	return !isWord(r)
}

func isWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func fold(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range strings.ToLower(value) {
		if mapped, ok := folded[r]; ok {
			b.WriteString(mapped)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// folded maps Serbian Latin and Cyrillic letters onto ASCII so a synonym matches either script.
var folded = map[rune]string{
	'č': "c", 'ć': "c", 'š': "s", 'ž': "z", 'đ': "d",
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'ђ': "d",
	'е': "e", 'ж': "z", 'з': "z", 'и': "i", 'ј': "j", 'к': "k",
	'л': "l", 'љ': "lj", 'м': "m", 'н': "n", 'њ': "nj", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'ћ': "c", 'у': "u",
	'ф': "f", 'х': "h", 'ц': "c", 'ч': "c", 'џ': "dz", 'ш': "s",
}
