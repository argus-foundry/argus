package db

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// HeraBoardAnnotation anchors feedback in revision-local rendered authored text.
// Quote is output only: clients cannot choose what text is delivered.
type HeraBoardAnnotation struct {
	AnnotationID     string `json:"annotation_id"`
	Type             string `json:"type"`
	StartPath        string `json:"start_path"`
	EndPath          string `json:"end_path"`
	StartOffset      int    `json:"start_offset"`
	EndOffset        int    `json:"end_offset"`
	SelectedTextHash string `json:"selected_text_hash"`
	Prefix           string `json:"prefix"`
	Suffix           string `json:"suffix"`
	Comment          string `json:"comment"`
	Quote            string `json:"quote,omitempty"`
	QuoteTruncated   bool   `json:"quote_truncated,omitempty"`
}

// Reject unsupported annotation operations (including replacement payloads)
// instead of accepting a successful receipt after silently dropping feedback.
func (a *HeraBoardAnnotation) UnmarshalJSON(raw []byte) error {
	type wireAnnotation HeraBoardAnnotation
	var wire wireAnnotation
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	*a = HeraBoardAnnotation(wire)
	return nil
}

type HeraBoardAnnotationOutcome struct {
	AnnotationID string `json:"annotation_id"`
	Outcome      string `json:"outcome"`
	Error        string `json:"error,omitempty"`
}

var boardInline = regexp.MustCompile("(`[^`]+`|\\*\\*[^*]+\\*\\*)")

// Match ECMAScript's whitespace and line terminators exactly: Go's \s and
// TrimSpace disagree with the page on BOM, NBSP, NEL and CRLF list endings.
const boardJSWhitespace = "\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"

var boardBullet = regexp.MustCompile(`^[\t\n\x0B\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}]*(- |\d+\. )([^\r\x{2028}\x{2029}]*)(?:[\r\x{2028}\x{2029}])?$`)

func boardInlineText(text string) string {
	return boardInline.ReplaceAllStringFunc(text, func(s string) string {
		if strings.HasPrefix(s, "**") {
			return boardInlineText(s[2 : len(s)-2])
		}
		return s[1 : len(s)-1]
	})
}

func boardRenderedText(text string, body bool) string {
	if !body {
		return boardInlineText(text)
	}
	var blocks []string
	paragraph := false
	for _, line := range strings.Split(text, "\n") {
		if strings.Trim(line, boardJSWhitespace) == "" {
			paragraph = false
			continue
		}
		if m := boardBullet.FindStringSubmatch(line); m != nil {
			blocks = append(blocks, boardInlineText(m[2]))
			paragraph = false
		} else if paragraph {
			blocks[len(blocks)-1] += " " + boardInlineText(line)
		} else {
			blocks = append(blocks, boardInlineText(line))
			paragraph = true
		}
	}
	return strings.Join(blocks, "\n")
}

type boardTextField struct{ path, text string }

func boardTextFields(i *HeraBoardItem) []boardTextField {
	fields := []boardTextField{{"body", boardRenderedText(i.Body, true)}}
	if s := i.Structured; s != nil {
		for _, o := range s.Options {
			for _, f := range []boardTextField{{"label", o.Label}, {"summary", o.Summary}, {"upside", o.Upside}, {"downside", o.Downside}} {
				if f.text != "" {
					fields = append(fields, boardTextField{"options." + o.ID + "." + f.path, boardRenderedText(f.text, false)})
				}
			}
			if s.Reason != "" && s.Recommended == o.ID {
				fields = append(fields, boardTextField{"reason", boardRenderedText(s.Reason, false)})
			}
		}
	}
	return fields
}

func annotationShapeError(a HeraBoardAnnotation) string {
	switch {
	case !boardFYIActionID.MatchString(a.AnnotationID):
		return "invalid annotation_id"
	case a.Type != "COMMENT" && a.Type != "DELETION":
		return "type must be COMMENT or DELETION"
	case len(a.Comment) > 4096 || !utf8.ValidString(a.Comment):
		return "comment exceeds 4 KiB or is not UTF-8"
	case a.Type == "COMMENT" && strings.TrimSpace(a.Comment) == "":
		return "COMMENT requires a comment"
	case utf8.RuneCountInString(a.Prefix) > 64 || utf8.RuneCountInString(a.Suffix) > 64:
		return "context exceeds 64 code points"
	case !utf8.ValidString(a.Prefix) || !utf8.ValidString(a.Suffix):
		return "invalid context"
	}
	return ""
}

// validateBoardAnnotations never reanchors: invalid text/hash/range returns an
// explicit per-annotation error and prevents the entire item's action.
func validateBoardAnnotations(i *HeraBoardItem, annotations []HeraBoardAnnotation) ([]HeraBoardAnnotation, []HeraBoardAnnotationOutcome, bool) {
	if len(annotations) == 0 {
		return nil, nil, true
	}
	fields := boardTextFields(i)
	allParts := make([]string, len(fields))
	for n, f := range fields {
		allParts[n] = f.text
	}
	seen := map[string]bool{}
	out := make([]HeraBoardAnnotation, len(annotations))
	copy(out, annotations)
	outcomes := make([]HeraBoardAnnotationOutcome, 0, len(out))
	valid := len(out) <= 20
	for n, a := range out {
		problem := annotationShapeError(a)
		if len(out) > 20 {
			problem = "maximum 20 annotations per item"
		}
		if seen[a.AnnotationID] {
			problem = "duplicate annotation_id"
		}
		seen[a.AnnotationID] = true
		start, end := -1, -1
		for j, f := range fields {
			if f.path == a.StartPath {
				start = j
			}
			if f.path == a.EndPath {
				end = j
			}
		}
		if start < 0 || end < start {
			problem = "invalid source-field paths"
		}
		selected, before, after := "", "", ""
		if problem == "" {
			parts := make([]string, 0, end-start+1)
			for j := start; j <= end; j++ {
				units := utf16.Encode([]rune(fields[j].text))
				lo, hi := 0, len(units)
				if j == start {
					lo = a.StartOffset
				}
				if j == end {
					hi = a.EndOffset
				}
				boundary := func(p int) bool {
					return p >= 0 && p <= len(units) && !(p > 0 && p < len(units) && units[p] >= 0xdc00 && units[p] <= 0xdfff)
				}
				if !boundary(lo) || !boundary(hi) || lo > hi {
					problem = "invalid UTF-16 offsets"
					break
				}
				parts = append(parts, string(utf16.Decode(units[lo:hi])))
				if j == start {
					before = string(utf16.Decode(units[:lo]))
				}
				if j == end {
					after = string(utf16.Decode(units[hi:]))
				}
			}
			selected = strings.Join(parts, "\n")
			if start > 0 {
				before = strings.Join(allParts[:start], "\n") + "\n" + before
			}
			if end+1 < len(fields) {
				after += "\n" + strings.Join(allParts[end+1:], "\n")
			}
			if problem == "" && (selected == "" || fmt.Sprintf("%x", sha256.Sum256([]byte(selected))) != a.SelectedTextHash) {
				problem = "selected text hash does not match rendered range"
			}
			if problem == "" && (!strings.HasSuffix(before, a.Prefix) || !strings.HasPrefix(after, a.Suffix)) {
				problem = "context does not match rendered range"
			}
		}
		outcome := HeraBoardAnnotationOutcome{AnnotationID: a.AnnotationID, Outcome: "accepted"}
		if problem != "" {
			outcome.Outcome, outcome.Error = "invalid", problem
			valid = false
		} else {
			out[n].Quote = boardUTF8Prefix(selected, 512)
			out[n].QuoteTruncated = len(selected) > 512
		}
		outcomes = append(outcomes, outcome)
	}
	return out, outcomes, valid
}

// ValidBoardAnnotationEnvelope bounds decoded feedback before any batch writes.
// Range/hash validation remains revision-local and yields per-annotation outcomes.
func ValidBoardAnnotationEnvelope(annotations []HeraBoardAnnotation) bool {
	if len(annotations) > 20 {
		return false
	}
	for _, a := range annotations {
		if len(a.Comment) > 4096 || !utf8.ValidString(a.Comment) || utf8.RuneCountInString(a.Prefix) > 64 || utf8.RuneCountInString(a.Suffix) > 64 || !utf8.ValidString(a.Prefix) || !utf8.ValidString(a.Suffix) {
			return false
		}
	}
	return true
}

func boardFeedback(note string, annotations []HeraBoardAnnotation, budget int, roleID int64, actionID string) (string, error) {
	pointer := fmt.Sprintf("Full feedback receipt: authenticated GET /api/hera/roles/%d/board/fyi-actions/%s\n", roleID, actionID)
	marker := "… [full: receipt above]"
	// Reserve one recovery URL per item. Every shortened quote/comment points
	// back to that URL, leaving room to represent all 20 annotations per item.
	per := (budget - len(pointer)) / (1 + len(annotations))
	clip := func(text string, limit int, force bool) string {
		if len(text) <= limit && !force {
			return text
		}
		return boardUTF8Prefix(text, limit-len(marker)) + marker
	}
	if per < 100 {
		return "", ErrHeraBoardInvalid
	}
	parts := []string{clip(note, per, false)}
	for n, a := range annotations {
		head := fmt.Sprintf("\n  %s #%d\n    Quote: ", a.Type, n+1)
		middle := "\n    Comment: "
		remaining := per - len(head) - len(middle)
		quoteBudget := remaining / 2
		commentBudget := remaining - quoteBudget
		parts = append(parts, head+clip(a.Quote, quoteBudget, a.QuoteTruncated)+middle+clip(a.Comment, commentBudget, false))
	}
	return pointer + strings.Join(parts, ""), nil
}

func boardUTF8Prefix(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	if limit < 0 {
		return ""
	}
	for limit > 0 && !utf8.ValidString(text[:limit]) {
		limit--
	}
	return text[:limit]
}
