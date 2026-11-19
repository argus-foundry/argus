package db

import (
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func decisionFixture() *HeraBoardStructure {
	return &HeraBoardStructure{Options: []HeraBoardOption{{ID: "retry", Label: "Retry"}, {ID: "wait", Label: "Wait"}}, Recommended: "retry", Reason: "Recover sooner"}
}

func TestBoardStructuredValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*HeraBoardStructure)
	}{
		{"one", func(s *HeraBoardStructure) { s.Options = s.Options[:1] }},
		{"duplicate", func(s *HeraBoardStructure) { s.Options[1].ID = "retry" }},
		{"reserved", func(s *HeraBoardStructure) { s.Options[1].ID = "other" }},
		{"id", func(s *HeraBoardStructure) { s.Options[1].ID = "UPPER" }},
		{"label", func(s *HeraBoardStructure) { s.Options[1].Label = " " }},
		{"long", func(s *HeraBoardStructure) { s.Options[1].Summary = strings.Repeat("x", 401) }},
		{"control", func(s *HeraBoardStructure) { s.Reason = "bad\x1b" }},
		{"recommendation", func(s *HeraBoardStructure) { s.Recommended = "missing" }},
		{"too many", func(s *HeraBoardStructure) {
			s.Options = append(s.Options, HeraBoardOption{ID: "c", Label: "C"}, HeraBoardOption{ID: "d", Label: "D"}, HeraBoardOption{ID: "e", Label: "E"})
		}},
		{"label cap", func(s *HeraBoardStructure) { s.Options[0].Label = strings.Repeat("a", 81) }},
		{"reason cap", func(s *HeraBoardStructure) { s.Reason = strings.Repeat("a", 401) }},
		{"serialized cap", func(s *HeraBoardStructure) {
			for _, id := range []string{"c", "d"} {
				s.Options = append(s.Options, HeraBoardOption{ID: id, Label: id})
			}
			for n := range s.Options {
				s.Options[n].Summary = strings.Repeat("😀", 400)
				s.Options[n].Upside = strings.Repeat("😀", 400)
				s.Options[n].Downside = strings.Repeat("😀", 400)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := decisionFixture()
			tc.change(s)
			_, err := encodeBoardStructure("decision", s)
			testutil.ErrorIs(t, err, ErrHeraBoardInvalid)
		})
	}
	_, err := encodeBoardStructure("fyi", decisionFixture())
	testutil.ErrorIs(t, err, ErrHeraBoardInvalid)
}

func TestBoardStructuredRoundTrip(t *testing.T) {
	d := heraTestDB(t)
	o := mkOrch(t, d, "decisions")
	c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
	i, err := d.PostHeraBoardItem(c.ID, "decision", "Choose", "Situation", decisionFixture())
	testutil.NoError(t, err)
	testutil.DeepEqual(t, i.Structured, decisionFixture())
	testutil.NoError(t, d.ReadHeraBoardItem(c.ID, i.ID))
	same, err := d.UpdateHeraBoardItem(c.ID, i.ID, i.Kind, i.Subject, i.Body, decisionFixture())
	testutil.NoError(t, err)
	testutil.Equal(t, same.Read, true)
	testutil.Equal(t, same.UpdatedAt, i.UpdatedAt)
	updated, err := d.UpdateHeraBoardItem(c.ID, i.ID, i.Kind, i.Subject, "New situation")
	testutil.NoError(t, err)
	testutil.DeepEqual(t, updated.Structured, i.Structured)
	cleared, err := d.UpdateHeraBoardItem(c.ID, i.ID, i.Kind, i.Subject, "New situation", nil)
	testutil.NoError(t, err)
	testutil.Nil(t, cleared.Structured)
	testutil.Equal(t, cleared.Read, false)
	testutil.Equal(t, cleared.UpdatedAt.After(updated.UpdatedAt), true)
	structure := decisionFixture()
	_, err = d.UpdateHeraBoardItem(c.ID, i.ID, i.Kind, i.Subject, i.Body, structure)
	testutil.NoError(t, err)
	testutil.NoError(t, d.ReadHeraBoardItem(c.ID, i.ID))
	structure.Options[0].Downside = "New downside"
	material, err := d.UpdateHeraBoardItem(c.ID, i.ID, i.Kind, i.Subject, i.Body, structure)
	testutil.NoError(t, err)
	testutil.Equal(t, material.Read, false)
	testutil.Equal(t, material.Structured.Options[0].Downside, "New downside")
}

func TestBoardAnswersTransaction(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[rollback], func(t *testing.T) {
			d := heraTestDB(t)
			o := mkOrch(t, d, "answers")
			c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
			i, err := d.PostHeraBoardItem(c.ID, "decision", "Choose", "Situation", decisionFixture())
			testutil.NoError(t, err)
			plain, err := d.PostHeraBoardItem(c.ID, "decision", "Explain", "Situation")
			testutil.NoError(t, err)
			if rollback {
				_, err = d.conn.Exec(`CREATE TRIGGER reject_answers BEFORE INSERT ON hera_messages BEGIN SELECT RAISE(ABORT,'test'); END`)
				testutil.NoError(t, err)
			}
			answers := []HeraBoardAnswer{{ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Choice: "retry"}, {ItemID: plain.ID, ExpectedUpdatedAt: plain.UpdatedAt, Choice: "other", Note: "Custom"}}
			results, err := d.AnswerHeraBoardItems(c.ID, answers)
			if rollback {
				testutil.Error(t, err)
				open, e := d.ListHeraBoardItems(c.ID, false)
				testutil.NoError(t, e)
				testutil.Equal(t, len(open), 2)
				return
			}
			testutil.NoError(t, err)
			testutil.Equal(t, results[0].Outcome, "resolved")
			testutil.Equal(t, results[1].Outcome, "resolved")
			msgs, err := d.HeraInbox(c.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(msgs), 1)
			testutil.Contains(t, msgs[0].Body, "Retry (the recommendation)")
			testutil.Contains(t, msgs[0].Body, "Custom")
			results, err = d.AnswerHeraBoardItems(c.ID, answers)
			testutil.NoError(t, err)
			testutil.Equal(t, results[0].Outcome, "already_resolved")
			msgs, err = d.HeraInbox(c.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(msgs), 1)
		})
	}
}

func TestBoardAnswerOutcomes(t *testing.T) {
	for _, tc := range []struct{ name, choice, note, want string }{{"unknown", "missing", "", "invalid"}, {"other missing note", "other", " ", "invalid"}, {"note cap", "retry", strings.Repeat("x", 4097), "invalid"}, {"valid", "wait", "", "resolved"}, {"conflict", "retry", "", "conflict"}, {"wrong role", "retry", "", "not_found"}, {"plain", "other", "Answer", "resolved"}} {
		t.Run(tc.name, func(t *testing.T) {
			d := heraTestDB(t)
			o := mkOrch(t, d, "outcomes")
			c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
			structure := decisionFixture()
			if tc.name == "plain" {
				structure = nil
			}
			i, err := d.PostHeraBoardItem(c.ID, "decision", "Choose", "Situation", structure)
			testutil.NoError(t, err)
			a := HeraBoardAnswer{ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Choice: tc.choice, Note: tc.note}
			role := c.ID
			if tc.name == "conflict" {
				_, err = d.UpdateHeraBoardItem(c.ID, i.ID, "decision", "Changed", "New")
				testutil.NoError(t, err)
			}
			if tc.name == "wrong role" {
				role++
			}
			results, err := d.AnswerHeraBoardItems(role, []HeraBoardAnswer{a})
			testutil.NoError(t, err)
			testutil.Equal(t, results[0].Outcome, tc.want)
			messages, err := d.HeraInbox(c.ID)
			testutil.NoError(t, err)
			want := 0
			if tc.want == "resolved" {
				want = 1
			}
			testutil.Equal(t, len(messages), want)
		})
	}
}

func TestBoardAnswerSummaryBound(t *testing.T) {
	d := heraTestDB(t)
	o := mkOrch(t, d, "bound")
	c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
	var answers []HeraBoardAnswer
	for range 16 {
		i, err := d.PostHeraBoardItem(c.ID, "decision", strings.Repeat("x", 240), "Body")
		testutil.NoError(t, err)
		answers = append(answers, HeraBoardAnswer{ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Choice: "other", Note: strings.Repeat("界", 1365) + "x"})
	}
	_, err := d.AnswerHeraBoardItems(c.ID, answers)
	testutil.NoError(t, err)
	messages, err := d.HeraInbox(c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(messages), 1)
	testutil.Equal(t, len(messages[0].Body) <= 65536, true)
	testutil.Equal(t, strings.Count(messages[0].Body, "[truncated — full note on board item #"), 16)
	items, err := d.ListHeraBoardItems(c.ID, true)
	testutil.NoError(t, err)
	for _, i := range items {
		testutil.Equal(t, len(i.ResolutionNote), 4096)
	}
}

func TestResidentApprovalRejectsDecisionAnswers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, call, now := approvalFixture(t)
	approval, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	items, err := d.ListHeraBoardItems(approval.RoleID, false)
	testutil.NoError(t, err)
	item := items[0]
	_, err = d.UpdateHeraBoardItem(approval.RoleID, item.ID, item.Kind, item.Subject, item.Body, decisionFixture())
	testutil.Error(t, err)
	results, err := d.AnswerHeraBoardItems(approval.RoleID, []HeraBoardAnswer{{ItemID: item.ID, ExpectedUpdatedAt: item.UpdatedAt, Choice: "other", Note: "Unauthorized approval"}})
	testutil.NoError(t, err)
	testutil.Equal(t, results[0].Outcome, "invalid")
	items, err = d.ListHeraBoardItems(approval.RoleID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(items), 1)
	testutil.Equal(t, items[0].ApprovalState, "pending")
}

func TestBoardAnswerEventsAfterCommit(t *testing.T) {
	d := heraTestDB(t)
	o := mkOrch(t, d, "events")
	c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
	one, err := d.PostHeraBoardItem(c.ID, "decision", "One", "Body")
	testutil.NoError(t, err)
	two, err := d.PostHeraBoardItem(c.ID, "decision", "Two", "Body")
	testutil.NoError(t, err)
	count := 0
	prev := events.SetSink(boardObservingSink{observe: func(e model.Event) {
		count++
		items, err := d.ListHeraBoardItems(c.ID, true)
		testutil.NoError(t, err)
		testutil.Equal(t, items[0].Resolved, true)
		testutil.Equal(t, items[1].Resolved, true)
		messages, err := d.HeraInbox(c.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, len(messages), 1)
	}})
	t.Cleanup(func() { events.SetSink(prev) })
	answers := []HeraBoardAnswer{{ItemID: one.ID, ExpectedUpdatedAt: one.UpdatedAt, Choice: "other", Note: "First"}, {ItemID: two.ID, ExpectedUpdatedAt: two.UpdatedAt, Choice: "other", Note: "Second"}}
	_, err = d.AnswerHeraBoardItems(c.ID, answers)
	testutil.NoError(t, err)
	testutil.Equal(t, count, 2)
	_, err = d.AnswerHeraBoardItems(c.ID, answers)
	testutil.NoError(t, err)
	testutil.Equal(t, count, 2)
}
