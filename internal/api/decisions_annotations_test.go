package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestAnnotatedResponsesHTTP(t *testing.T) {
	for _, kind := range []string{"decision", "fyi"} {
		t.Run(kind, func(t *testing.T) {
			s, d, c, _, _ := boardRESTFixture(t)
			i, err := d.PostHeraBoardItem(c.ID, kind, "Annotation", "**😀 Body**")
			testutil.NoError(t, err)
			session, _, err := s.decisions.mint(c.ID, true)
			testutil.NoError(t, err)
			annotation := db.HeraBoardAnnotation{AnnotationID: "one", Type: "DELETION", StartPath: "body", EndPath: "body", StartOffset: 0, EndOffset: 7, SelectedTextHash: fmt.Sprintf("%x", sha256.Sum256([]byte("😀 Body"))), Quote: "forged"}
			payload := func(id string, annotations []db.HeraBoardAnnotation) string {
				var p any
				if kind == "decision" {
					p = map[string]any{"answers": []db.HeraBoardAnswer{{ActionID: id, ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "comment", Annotations: annotations}}}
				} else {
					p = map[string]any{"actions": []db.HeraBoardFYIAction{{ActionID: id, ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "comment", Annotations: annotations}}}
				}
				raw, e := json.Marshal(p)
				testutil.NoError(t, e)
				return string(raw)
			}
			route := "answers"
			if kind == "fyi" {
				route = "fyi-actions"
			}
			base := fmt.Sprintf("/api/hera/roles/%d/board/", c.ID)
			body := payload("response", []db.HeraBoardAnnotation{annotation})
			unsupported := strings.Replace(body, `"annotation_id"`, `"replacement":"unsupported","annotation_id"`, 1)
			testutil.Equal(t, decideCall(t, s, "POST", base+route, unsupported, session).Code, 400)
			for range 2 {
				w := decideCall(t, s, "POST", base+route, body, session)
				testutil.Equal(t, w.Code, 200)
				testutil.Contains(t, w.Body.String(), `"outcome":"commented"`)
			}
			testutil.Equal(t, len(mustMsgs(t, d, c.ID)), 1)
			receipt := decideCall(t, s, "GET", base+"fyi-actions/response", "", "test-token")
			testutil.Equal(t, receipt.Code, 200)
			testutil.Contains(t, receipt.Body.String(), `"quote":"😀 Body"`)
			testutil.Equal(t, strings.Contains(receipt.Body.String(), "forged"), false)
			// A new server has no page credentials or response cache. Both receipt
			// recovery and master-authenticated replay use the in-memory DB alone.
			restarted := New(d, s.runner, "test-token", nil, nil)
			close(restarted.stopCh)
			<-restarted.idleWatcherDone
			recovered := decideCall(t, restarted, "GET", base+"fyi-actions/response", "", "test-token")
			testutil.Equal(t, recovered.Body.String(), receipt.Body.String())
			replayed := decideCall(t, restarted, "POST", base+route, body, "test-token")
			testutil.Equal(t, replayed.Code, 200)
			testutil.Contains(t, replayed.Body.String(), `"outcome":"commented"`)
			testutil.Equal(t, len(mustMsgs(t, d, c.ID)), 1)
			testutil.Equal(t, decideCall(t, s, "GET", base+"fyi-actions/response", "", session).Code, 403)
			annotation.SelectedTextHash = strings.Repeat("0", 64)
			invalid := decideCall(t, s, "POST", base+route, payload("bad_hash", []db.HeraBoardAnnotation{annotation}), session)
			testutil.Equal(t, invalid.Code, 200)
			testutil.Contains(t, invalid.Body.String(), `"annotation_outcomes"`)
			testutil.Contains(t, invalid.Body.String(), "hash does not match")
			duplicate := decideCall(t, s, "POST", base+route, payload("duplicate", []db.HeraBoardAnnotation{annotation, annotation}), session)
			testutil.Equal(t, duplicate.Code, 200)
			testutil.Contains(t, duplicate.Body.String(), "duplicate annotation_id")
			annotation.Type = "COMMENT"
			annotation.Comment = strings.Repeat("界", 1366)
			testutil.Equal(t, decideCall(t, s, "POST", base+route, payload("oversize", []db.HeraBoardAnnotation{annotation}), session).Code, 400)
			// Validate decoder EOF and bounded trailing bytes before any commit.
			testutil.Equal(t, decideCall(t, s, "POST", base+route, body+" {}", session).Code, 400)
			testutil.Equal(t, decideCall(t, s, "POST", base+route, body+strings.Repeat(" ", boardResponseBodyMaxBytes), session).Code, 400)
			testutil.Equal(t, len(mustMsgs(t, d, c.ID)), 1)
		})
	}
}
