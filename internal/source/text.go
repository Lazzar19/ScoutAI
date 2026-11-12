package source

import (
	"html"
	"strings"
	"unicode"
)

// htmlText turns a Greenhouse description into plain text.
// The list endpoint encodes tags as entities, so the text is unescaped before the tags are removed.
func htmlText(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	decoded := html.UnescapeString(raw)
	var b strings.Builder
	inTag := false
	for _, r := range decoded {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			if inTag {
				inTag = false
				b.WriteByte(' ')
			}
		case !inTag:
			b.WriteRune(r)
		}
	}
	return collapseSpace(html.UnescapeString(b.String()))
}

func collapseSpace(value string) string {
	fields := strings.FieldsFunc(value, unicode.IsSpace)
	return strings.Join(fields, " ")
}
