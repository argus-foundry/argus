package db

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestAnnotatedAnswersReplayOutboxAndPartialFailure(t *testing.T) {
	d, c, _ := fyiFixture(t)
	one, err := d.PostHeraBoardItem(c.ID, "decision", "Choose", "Body", decisionFixture())
	testutil.NoError(t, err)
	two, err := d.PostHeraBoardItem(c.ID, "decision", "Explain", "Body")
	testutil.NoError(t, err)
	testutil.NoError(t, d.ReadHeraBoardItem(c.ID, one.ID))
	a := HeraBoardAnswer{ActionID: "resolved", ItemID: one.ID, ExpectedUpdatedAt: one.UpdatedAt, Choice: "retry", Annotations: []HeraBoardAnnotation{annotationFor("Body")}}
	b := HeraBoardAnswer{ActionID: "missing_note", ItemID: two.ID, ExpectedUpdatedAt: two.UpdatedAt, Choice: "other", Annotations: []HeraBoardAnnotation{annotationFor("Body")}}
	sink := sinkFor(t)
	results, err := d.AnswerHeraBoardItems(c.ID, []HeraBoardAnswer{b, a})
	testutil.NoError(t, err)
	testutil.Equal(t, results[0].Outcome, "resolved")
	testutil.Equal(t, results[1].Outcome, "invalid")
	testutil.Equal(t, len(sink.board()), 1)
	receipt, err := d.HeraBoardFYIReceipt(c.ID, a.ActionID)
	testutil.NoError(t, err)
	testutil.Equal(t, receipt.Item.Read, true)
	testutil.Equal(t, receipt.Choice, "retry")
	replay, err := d.AnswerHeraBoardItems(c.ID, []HeraBoardAnswer{a})
	testutil.NoError(t, err)
	testutil.DeepEqual(t, replay[0], results[0])
	testutil.Equal(t, len(sink.board()), 1)
	for _, field := range []string{"choice", "note", "annotation", "revision"} {
		t.Run(field, func(t *testing.T) {
			changed := a
			changed.Annotations = append([]HeraBoardAnnotation(nil), a.Annotations...)
			switch field {
			case "choice":
				changed.Choice = "wait"
			case "note":
				changed.Note = "Changed"
			case "annotation":
				changed.Annotations[0].Comment = "Changed"
			case "revision":
				changed.ExpectedUpdatedAt = two.UpdatedAt
			}
			out, err := d.AnswerHeraBoardItems(c.ID, []HeraBoardAnswer{changed})
			testutil.NoError(t, err)
			testutil.Equal(t, out[0].Outcome, "invalid")
		})
	}
	pending, err := d.PendingHeraBoardNotices()
	testutil.NoError(t, err)
	testutil.Equal(t, len(pending), 0)
	_, err = d.CreateHeraBinding(CreateHeraBindingInput{RoleID: c.ID, ArgusTaskID: "task", WorktreePath: "/wt"})
	testutil.NoError(t, err)
	pending, err = d.PendingHeraBoardNotices()
	testutil.NoError(t, err)
	testutil.Equal(t, len(pending), 1)
	testutil.Contains(t, pending[0].Body, "COMMENT #1")
	testutil.NoError(t, d.MarkHeraMessageDelivered(pending[0].ID, HeraDeliveryIdleSubmit))
	pending, err = d.PendingHeraBoardNotices()
	testutil.NoError(t, err)
	testutil.Equal(t, len(pending), 0)
}

func annotationFor(text string) HeraBoardAnnotation {
	return HeraBoardAnnotation{AnnotationID: "a1", Type: "COMMENT", StartPath: "body", EndPath: "body", StartOffset: 0, EndOffset: len(utf16.Encode([]rune(text))), SelectedTextHash: fmt.Sprintf("%x", sha256.Sum256([]byte(text))), Comment: "Please clarify"}
}

func TestAnnotatedResponseRollback(t *testing.T) {
	for _, stage := range []string{"receipt", "summary", "commit"} {
		t.Run(stage, func(t *testing.T) {
			d, c, _ := fyiFixture(t)
			i, err := d.PostHeraBoardItem(c.ID, "decision", "Choose", "Body", decisionFixture())
			testutil.NoError(t, err)
			statements := map[string]string{
				"receipt": `CREATE TRIGGER reject_response BEFORE INSERT ON hera_board_fyi_receipts BEGIN SELECT RAISE(ABORT,'failed'); END`,
				"summary": `CREATE TRIGGER reject_response BEFORE INSERT ON hera_messages BEGIN SELECT RAISE(ABORT,'failed'); END`,
				"commit":  `CREATE TABLE response_guard(role_id INTEGER REFERENCES hera_roles(id) DEFERRABLE INITIALLY DEFERRED); CREATE TRIGGER reject_response AFTER INSERT ON hera_messages BEGIN INSERT INTO response_guard VALUES(-1); END`,
			}
			_, err = d.conn.Exec(statements[stage])
			testutil.NoError(t, err)
			sink := sinkFor(t)
			_, err = d.AnswerHeraBoardItems(c.ID, []HeraBoardAnswer{{ActionID: "rollback", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Choice: "retry", Annotations: []HeraBoardAnnotation{annotationFor("Body")}}})
			testutil.Error(t, err)
			items, err := d.ListHeraBoardItems(c.ID, true)
			testutil.NoError(t, err)
			for _, item := range items {
				if item.ID == i.ID {
					testutil.DeepEqual(t, item, i)
				}
			}
			_, err = d.HeraBoardFYIReceipt(c.ID, "rollback")
			testutil.ErrorIs(t, err, ErrHeraBoardNotFound)
			messages, err := d.HeraInbox(c.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(messages), 0)
			testutil.Equal(t, len(sink.board()), 0)
		})
	}
}

func TestAnnotatedSummaryBudget(t *testing.T) {
	for _, kind := range []string{"decision", "fyi"} {
		t.Run(kind, func(t *testing.T) {
			d, c, _ := fyiFixture(t)
			var answers []HeraBoardAnswer
			var actions []HeraBoardFYIAction
			for n := range 16 {
				i, err := d.PostHeraBoardItem(c.ID, kind, strings.Repeat("s", 240), strings.Repeat("😀", 300))
				testutil.NoError(t, err)
				var annotations []HeraBoardAnnotation
				for j := range 20 {
					a := annotationFor(i.Body)
					a.AnnotationID = fmt.Sprintf("a%d", j)
					a.Comment = strings.Repeat("界", 1365) + "!"
					annotations = append(annotations, a)
				}
				actionID := fmt.Sprintf("%0128d", n)
				actions = append(actions, HeraBoardFYIAction{ActionID: actionID, ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "comment", Note: strings.Repeat("x", 4096), Annotations: annotations})
				answers = append(answers, HeraBoardAnswer{ActionID: actionID, ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "comment", Note: strings.Repeat("x", 4096), Annotations: annotations})
			}
			if kind == "decision" {
				_, err := d.AnswerHeraBoardItems(c.ID, answers)
				testutil.NoError(t, err)
			} else {
				_, err := d.FYIHeraBoardItems(c.ID, actions)
				testutil.NoError(t, err)
			}
			messages, err := d.HeraInbox(c.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(messages), 1)
			testutil.Equal(t, len(messages[0].Body) <= 65536, true)
			testutil.Equal(t, utf8.ValidString(messages[0].Body), true)
			testutil.Equal(t, strings.Count(messages[0].Body, "COMMENT #"), 320)
			testutil.Equal(t, strings.Count(messages[0].Body, "Quote: "), 320)
			testutil.Equal(t, strings.Count(messages[0].Body, "Full feedback receipt:"), 16)
			receipt, err := d.HeraBoardFYIReceipt(c.ID, actions[0].ActionID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(receipt.Annotations), 20)
			testutil.Equal(t, len(receipt.Annotations[0].Comment), 4096)
			testutil.Equal(t, len(receipt.Annotations[0].Quote), 512)
			testutil.Equal(t, receipt.Annotations[0].QuoteTruncated, true)
		})
	}
}

func TestAnnotationEnvelopeAndUTF16(t *testing.T) {
	for _, name := range []string{"deletion", "duplicate", "cap", "comment bytes", "surrogate", "backwards", "cross field"} {
		t.Run(name, func(t *testing.T) {
			a := annotationFor("😀😀")
			annotations := []HeraBoardAnnotation{a}
			i := &HeraBoardItem{Body: "😀😀"}
			want := false
			switch name {
			case "deletion":
				annotations[0].Type = "DELETION"
				annotations[0].Comment = ""
				want = true
			case "duplicate":
				annotations = append(annotations, a)
			case "cap":
				for range 20 {
					annotations = append(annotations, a)
				}
			case "comment bytes":
				annotations[0].Comment = strings.Repeat("界", 1366)
			case "surrogate":
				annotations[0].EndOffset = 1
			case "backwards":
				annotations[0].StartOffset = 4
				annotations[0].EndOffset = 2
			case "cross field":
				annotations[0].EndPath = "reason"
			}
			_, _, valid := validateBoardAnnotations(i, annotations)
			testutil.Equal(t, valid, want)
		})
	}
}

func TestAnnotationSharedVectors(t *testing.T) {
	var vectors []struct {
		Name                     string
		Body                     string
		Structured               *HeraBoardStructure
		Fields                   []struct{ Path, Text string }
		Selection                HeraBoardAnnotation
		Selected, Prefix, Suffix string
	}
	raw, err := os.ReadFile("testdata/decide-annotation-vectors.json")
	testutil.NoError(t, err)
	testutil.NoError(t, json.Unmarshal(raw, &vectors))
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			i := &HeraBoardItem{Body: v.Body, Structured: v.Structured}
			fields := boardTextFields(i)
			testutil.Equal(t, len(fields), len(v.Fields))
			for n, f := range fields {
				testutil.Equal(t, f.path, v.Fields[n].Path)
				testutil.Equal(t, f.text, v.Fields[n].Text)
			}
			a := v.Selection
			a.AnnotationID, a.Type, a.Comment = "vector", "COMMENT", "Check"
			a.SelectedTextHash = fmt.Sprintf("%x", sha256.Sum256([]byte(v.Selected)))
			a.Prefix, a.Suffix = v.Prefix, v.Suffix
			annotations, outcomes, valid := validateBoardAnnotations(i, []HeraBoardAnnotation{a})
			testutil.Equal(t, valid, true)
			testutil.Equal(t, outcomes[0].Outcome, "accepted")
			testutil.Equal(t, annotations[0].Quote, v.Selected)
		})
	}
}

func TestAnnotatedDecisionReceipt(t *testing.T) {
	d, c, _ := fyiFixture(t)
	i, err := d.PostHeraBoardItem(c.ID, "decision", "Choose", "**Situation**")
	testutil.NoError(t, err)
	a := HeraBoardAnswer{ActionID: "response", Action: "comment", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Annotations: []HeraBoardAnnotation{annotationFor("Situation")}}
	results, err := d.AnswerHeraBoardItems(c.ID, []HeraBoardAnswer{a})
	testutil.NoError(t, err)
	testutil.Equal(t, results[0].Outcome, "commented")
	receipt, err := d.HeraBoardFYIReceipt(c.ID, a.ActionID)
	testutil.NoError(t, err)
	testutil.Equal(t, receipt.Annotations[0].Quote, "Situation")
	testutil.Equal(t, receipt.Item.Resolved, false)
	testutil.Equal(t, receipt.Item.UpdatedAt, i.UpdatedAt)
	testutil.NoError(t, d.ResolveHeraBoardItem(c.ID, i.ID, "agent", "done"))
	replay, err := d.AnswerHeraBoardItems(c.ID, []HeraBoardAnswer{a})
	testutil.NoError(t, err)
	testutil.DeepEqual(t, replay, results)
	a.ActionID = "fresh"
	closed, err := d.AnswerHeraBoardItems(c.ID, []HeraBoardAnswer{a})
	testutil.NoError(t, err)
	testutil.Equal(t, closed[0].Outcome, "invalid")
	messages, err := d.HeraInbox(c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(messages), 1)
	testutil.Contains(t, messages[0].Body, "COMMENT")
	testutil.Contains(t, messages[0].Body, "Situation")
}

func TestAnnotationValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*HeraBoardAnnotation)
	}{
		{"hash", func(a *HeraBoardAnnotation) { a.SelectedTextHash = "bad" }},
		{"offset", func(a *HeraBoardAnnotation) { a.EndOffset = 999 }},
		{"path", func(a *HeraBoardAnnotation) { a.StartPath = "subject" }},
		{"empty comment", func(a *HeraBoardAnnotation) { a.Comment = " " }},
		{"context", func(a *HeraBoardAnnotation) { a.Prefix = "wrong" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, c, i := fyiFixture(t)
			a := annotationFor(i.Body)
			tc.change(&a)
			results, err := d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{{ActionID: "bad", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "comment", Annotations: []HeraBoardAnnotation{a}}})
			testutil.NoError(t, err)
			testutil.Equal(t, results[0].Outcome, "invalid")
			testutil.Equal(t, len(results[0].AnnotationOutcomes), 1)
			testutil.Equal(t, results[0].AnnotationOutcomes[0].Outcome, "invalid")
			_, err = d.HeraBoardFYIReceipt(c.ID, "bad")
			testutil.ErrorIs(t, err, ErrHeraBoardNotFound)
		})
	}
}
