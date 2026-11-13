package source

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html/charset"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

const (
	// MessageParsed means the email was read. It may contain zero listings.
	MessageParsed = "parsed"
	// MessageFailed means this email could not be read. Later emails still proceed.
	MessageFailed = "failed"

	maxMessageBytes = 2 << 20
)

var (
	jobURLPattern  = regexp.MustCompile(`(?i)https?://(?:[a-z0-9.-]+\.)?linkedin\.com/(?:comm/)?jobs/view/(\d+)(?:[?#][^\s<>"']*)?`)
	viewJobPrefix  = regexp.MustCompile(`(?i)^view job:\s*`)
	connectionLine = regexp.MustCompile(`(?i)^\d+ connections?$`)
	separatorLine  = regexp.MustCompile(`^-{5,}$`)
)

// ParseLinkedInAlert reads one RFC822 job-alert email into preview listings.
// The description stays empty. A snippet in the email is not stored as the job text.
// This function does not fetch the LinkedIn URL.
func ParseLinkedInAlert(raw []byte) ([]DiscoveredPosting, error) {
	plain, htmlBody, posted, err := splitMessage(raw)
	if err != nil {
		return nil, err
	}
	postings, untitled := parsePlain(plain, posted)
	if len(postings) == 0 {
		htmlPostings, htmlUntitled := parseHTML(htmlBody, posted)
		postings = htmlPostings
		untitled = append(untitled, htmlUntitled...)
	}
	if len(postings) == 0 && len(untitled) > 0 {
		return nil, fmt.Errorf("linkedin alert listings had no title")
	}
	return postings, nil
}

func splitMessage(raw []byte) (string, string, *time.Time, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return "", "", nil, fmt.Errorf("read message")
	}
	var posted *time.Time
	if date, err := msg.Header.Date(); err == nil {
		utc := date.UTC()
		posted = &utc
	}
	var plain, htmlBody string
	if err := collectParts(msg.Body, msg.Header, true, &plain, &htmlBody); err != nil {
		return "", "", nil, err
	}
	return plain, htmlBody, posted, nil
}

func collectParts(r io.Reader, header mail.Header, decodeCTE bool, plain, htmlBody *string) error {
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}
	if strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		if params["boundary"] == "" {
			return fmt.Errorf("multipart message has no boundary")
		}
		reader := multipart.NewReader(r, params["boundary"])
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("read message part")
			}
			err = collectParts(part, mail.Header(part.Header), false, plain, htmlBody)
			part.Close()
			if err != nil {
				return err
			}
		}
	}
	reader := r
	if decodeCTE {
		reader = decodeTransfer(r, header.Get("Content-Transfer-Encoding"))
	}
	body, err := readLimited(reader, header.Get("Content-Type"))
	if err != nil {
		return err
	}
	switch strings.ToLower(mediaType) {
	case "text/html":
		if *htmlBody == "" {
			*htmlBody = body
		}
	case "text/plain":
		if *plain == "" {
			*plain = body
		}
	}
	return nil
}

func decodeTransfer(r io.Reader, encoding string) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, r)
	default:
		return r
	}
}

func readLimited(r io.Reader, contentType string) (string, error) {
	reader := r
	if strings.Contains(strings.ToLower(contentType), "charset=") {
		decoded, err := charset.NewReader(r, contentType)
		if err != nil {
			return "", fmt.Errorf("read message charset")
		}
		reader = decoded
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxMessageBytes+1))
	if err != nil {
		return "", fmt.Errorf("read message body")
	}
	if len(raw) > maxMessageBytes {
		return "", fmt.Errorf("message is too large")
	}
	return string(raw), nil
}

func parsePlain(body string, posted *time.Time) ([]DiscoveredPosting, []string) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var postings []DiscoveredPosting
	var untitled []string
	seen := map[string]bool{}
	for i, line := range lines {
		id, original, ok := jobLine(line)
		if !ok {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		content := linesBefore(lines, i)
		if len(content) == 0 {
			untitled = append(untitled, id)
			continue
		}
		title := content[0]
		company := ""
		location := ""
		if len(content) > 1 {
			company = content[1]
		}
		if len(content) > 2 {
			location = content[2]
		}
		if location == "" {
			if left, right, ok := splitMiddot(company); ok {
				company, location = left, right
			}
		}
		postings = append(postings, postingFrom(id, title, company, location, original, posted))
	}
	return postings, untitled
}

func linesBefore(lines []string, index int) []string {
	var content []string
	for i := index - 1; i >= 0; i-- {
		line := collapseSpace(lines[i])
		if line == "" {
			if len(content) > 0 {
				break
			}
			continue
		}
		if separatorLine.MatchString(line) || jobLineIs(line) {
			break
		}
		if noiseLine(line) {
			continue
		}
		content = append([]string{line}, content...)
		if len(content) == 3 {
			break
		}
	}
	return content
}

func jobLineIs(line string) bool {
	_, _, ok := jobLine(line)
	return ok
}

func jobLine(line string) (string, string, bool) {
	trimmed := collapseSpace(line)
	without := viewJobPrefix.ReplaceAllString(trimmed, "")
	without = strings.TrimRight(strings.TrimSpace(without), ".,);>")
	original := jobURLPattern.FindString(without)
	if original == "" {
		return "", "", false
	}
	original = strings.TrimRight(original, ".,);>")
	if strings.TrimSpace(strings.Replace(without, original, "", 1)) != "" {
		return "", "", false
	}
	id := linkedInJobID(original)
	if id == "" {
		return "", "", false
	}
	return id, original, true
}

func parseHTML(body string, posted *time.Time) ([]DiscoveredPosting, []string) {
	if strings.TrimSpace(body) == "" {
		return nil, nil
	}
	matches := anchorPattern.FindAllStringSubmatchIndex(body, -1)
	type hit struct {
		start int
		end   int
		href  string
		inner string
	}
	var hits []hit
	for _, match := range matches {
		href := anchorGroup(body, match, 1)
		if href == "" {
			href = anchorGroup(body, match, 2)
		}
		href = strings.TrimSpace(html.UnescapeString(href))
		if strings.HasPrefix(href, "//") {
			href = "https:" + href
		}
		if linkedInJobID(href) == "" {
			continue
		}
		hits = append(hits, hit{
			start: match[0],
			end:   match[1],
			href:  href,
			inner: anchorGroup(body, match, 3),
		})
	}
	var postings []DiscoveredPosting
	var untitled []string
	seen := map[string]bool{}
	for i, item := range hits {
		id := linkedInJobID(item.href)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		limit := len(body)
		if i+1 < len(hits) {
			limit = hits[i+1].start
		}
		title := strings.Join(htmlLines(item.inner), " ")
		var follow []string
		for _, line := range htmlLines(body[item.end:limit]) {
			if noiseLine(line) || strings.EqualFold(line, title) {
				continue
			}
			follow = append(follow, line)
		}
		if title == "" {
			for _, line := range follow {
				if _, _, ok := splitMiddot(line); ok {
					continue
				}
				title = line
				break
			}
		}
		if title == "" {
			untitled = append(untitled, id)
			continue
		}
		company, location := splitCompanyLocation(follow, title)
		postings = append(postings, postingFrom(id, title, company, location, item.href, posted))
	}
	return postings, untitled
}

var anchorPattern = regexp.MustCompile(`(?is)<a\b[^>]*?\bhref\s*=\s*(?:"([^"]*)"|'([^']*)')[^>]*>(.*?)</a>`)

func anchorGroup(body string, match []int, group int) string {
	start, end := match[2*group], match[2*group+1]
	if start < 0 || end < 0 {
		return ""
	}
	return body[start:end]
}

func htmlLines(fragment string) []string {
	var b strings.Builder
	inTag := false
	for _, r := range fragment {
		switch {
		case r == '<':
			inTag = true
			b.WriteByte('\n')
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	var lines []string
	for _, line := range strings.Split(b.String(), "\n") {
		line = collapseSpace(html.UnescapeString(line))
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitCompanyLocation(lines []string, title string) (string, string) {
	for _, line := range lines {
		if strings.EqualFold(line, title) || noiseLine(line) {
			continue
		}
		if left, right, ok := splitMiddot(line); ok {
			return left, right
		}
	}
	return "", ""
}

func splitMiddot(line string) (string, string, bool) {
	for _, sep := range []string{" · ", " • ", " | "} {
		left, right, ok := strings.Cut(line, sep)
		if ok && strings.TrimSpace(left) != "" && strings.TrimSpace(right) != "" {
			return strings.TrimSpace(left), strings.TrimSpace(right), true
		}
	}
	return "", "", false
}

func noiseLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	switch {
	case lower == "":
		return true
	case strings.HasPrefix(lower, "view job"):
		return true
	case strings.HasPrefix(lower, "see all jobs"):
		return true
	case strings.HasPrefix(lower, "this company is actively"):
		return true
	case strings.HasPrefix(lower, "your job alert"):
		return true
	case strings.Contains(lower, "you'll receive notifications") || strings.Contains(lower, "you’ll receive notifications"):
		return true
	case connectionLine.MatchString(line) || separatorLine.MatchString(line):
		return true
	default:
		return false
	}
}

func linkedInJobID(raw string) string {
	match := jobURLPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func postingFrom(id, title, company, location, original string, posted *time.Time) DiscoveredPosting {
	jobID := id
	posting := DiscoveredPosting{
		ExternalID:   &jobID,
		OriginalURL:  strings.TrimSpace(original),
		CanonicalURL: "https://www.linkedin.com/jobs/view/" + id,
		Kind:         domain.KindListing,
		Completeness: domain.CompletenessPreview,
		Title:        title,
		PostedAt:     posted,
	}
	if cleaned := collapseSpace(company); cleaned != "" {
		posting.Company = &cleaned
	}
	if cleaned := collapseSpace(location); cleaned != "" {
		posting.Locations = []domain.Location{{Raw: cleaned}}
	}
	return posting
}
