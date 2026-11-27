package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// openBoardDecisions runs entirely off the UI thread; its caller queues only
// the final status. Never pass the credential URL through openURL, which logs it.
func (a *App) openBoardDecisions(roleID, focusItemID int64) string {
	if a.heraPage.IsRemote() {
		return ""
	}
	uxlog.Log("[board] browser open attempt role=%d focus=%d", roleID, focusItemID)
	if roleID == 0 {
		uxlog.Log("[board] browser no coordinator")
		return "No open decisions"
	}
	if !a.db.Config().API.Enabled {
		uxlog.Log("[board] browser API disabled role=%d", roleID)
		return "Turn the API on in Settings to answer decisions in the browser"
	}
	client, err := a.maintenanceClientFactory()
	if err != nil {
		uxlog.Log("[board] browser client unavailable role=%d", roleID)
		return "Turn the API on in Settings to answer decisions in the browser"
	}
	var link struct{ Path, Ticket string }
	body := fmt.Sprintf(`{"focus_item_id":%d}`, focusItemID)
	err = client.do(context.Background(), "POST", fmt.Sprintf("/api/hera/roles/%d/board/decision-link", roleID), strings.NewReader(body), &link)
	if err != nil {
		if strings.Contains(err.Error(), "no_decisions") {
			uxlog.Log("[board] browser no decisions role=%d", roleID)
			return "No open decisions"
		}
		uxlog.Log("[board] browser mint failed role=%d", roleID)
		return "Could not open decisions; check the API in Settings"
	}
	// Treat the response as a local daemon path, never an arbitrary open scheme.
	if !strings.HasPrefix(link.Path, "/decide?") || link.Ticket == "" {
		uxlog.Log("[board] browser invalid link role=%d", roleID)
		return "Could not open decisions"
	}
	if err = browserOpener(client.baseURL + link.Path + "#t=" + link.Ticket); err != nil {
		uxlog.Log("[board] browser launch failed role=%d", roleID)
		return "Could not launch the browser"
	}
	uxlog.Log("[board] browser opened role=%d", roleID)
	return "Decisions opened in browser"
}

func (a *App) confirmResidentApproval(d *db.DB, item *db.HeraBoardItem, approve bool) {
	action := "Deny"
	message := fmt.Sprintf("Deny the exact call shown on board #%d? Solo will be told to continue other work.", item.ID)
	if approve {
		action = "Approve once"
		message = fmt.Sprintf("Approve the exact call shown on board #%d once? The agent then has %d minutes to run it; a relaunch first voids the approval.", item.ID, int(db.ResidentApprovalWindow/time.Minute))
	}
	a.openHeraConfirm(action, message, func() {
		go func() {
			launch, readErr := agent.ReadResidentApprovalLaunch()
			var err error
			if approve && readErr != nil {
				err = readErr
			} else {
				err = d.DecideResidentApproval(item.ID, item.UpdatedAt, launch.Generation, approve, time.Now())
			}
			uxlog.Log("[resident-approval] human decision item=%d approve=%t err=%v", item.ID, approve, err)
			a.tapp.QueueUpdateDraw(func() {
				if err != nil {
					// A state error already explains what happened (already
					// approved, lapsed, superseded...); it is not a failure
					// of this click, so it is not prefixed as one.
					var state *db.ApprovalStateError
					if errors.As(err, &state) {
						a.statusbar.SetError("Not applied: " + err.Error())
					} else {
						a.statusbar.SetError("Approval failed: " + err.Error())
					}
					a.heraPage.Refresh()
					return
				}
				a.heraPage.Refresh()
			})
		}()
	})
}

func (a *App) mutateHeraBoard(d *db.DB, roleID, itemID int64, updatedAt time.Time, dismiss bool) {
	var err error
	if dismiss {
		err = d.ResolveHeraBoardItem(roleID, itemID, "human", "")
	} else {
		err = d.ReadHeraBoardItemAt(roleID, itemID, updatedAt)
	}
	uxlog.Log("[board] human mutation role=%d item=%d dismiss=%v err=%v", roleID, itemID, dismiss, err)
	a.tapp.QueueUpdateDraw(func() {
		if err != nil {
			a.statusbar.SetError("Board update failed: " + err.Error())
			return
		}
		a.heraPage.Refresh()
	})
}

// completeHeraBoardItem resolves an action (done) or todo (done/dropped)
// through the shared batch path at the revision the operator saw. It runs off
// the UI thread; command text is never logged.
func (a *App) completeHeraBoardItem(d *db.DB, item *db.HeraBoardItem, outcome string) {
	actionID := fmt.Sprintf("tui-%d-%d", item.ID, time.Now().UnixNano())
	results, err := d.FYIHeraBoardItems(item.RoleID, []db.HeraBoardFYIAction{{
		ActionID:          actionID,
		ItemID:            item.ID,
		ExpectedUpdatedAt: item.UpdatedAt,
		Action:            outcome,
	}})
	result := ""
	if err == nil && len(results) == 1 {
		result = results[0].Outcome
	}
	uxlog.Log("[board] complete role=%d item=%d kind=%s outcome=%s result=%q err=%v", item.RoleID, item.ID, item.Kind, outcome, result, err)
	a.tapp.QueueUpdateDraw(func() {
		switch {
		case err != nil:
			a.statusbar.SetError("Board update failed: " + err.Error())
		case result == "resolved":
			a.statusbar.SetInfo(fmt.Sprintf("#%d marked %s", item.ID, outcome))
			a.heraPage.Refresh()
		case result == "conflict":
			a.statusbar.SetError(fmt.Sprintf("#%d changed; review it and try again", item.ID))
			a.heraPage.Refresh()
		case result == "already_resolved":
			a.statusbar.SetInfo(fmt.Sprintf("#%d is already resolved", item.ID))
			a.heraPage.Refresh()
		default:
			a.statusbar.SetError(fmt.Sprintf("#%d not applied (%s)", item.ID, result))
		}
	})
}

// copyBoardCommand puts the exact stored step on the clipboard; nothing runs.
func (a *App) copyBoardCommand(item *db.HeraBoardItem) {
	uxlog.Log("[board] copy item=%d bytes=%d", item.ID, len(item.Command))
	a.copyToClipboard(item.Command, "Copied step", nil)
}

// deferHeraBoardItem uses the same human REST mutation as web, off the UI thread.
func (a *App) deferHeraBoardItem(item *db.HeraBoardItem, deferred bool) {
	client, err := a.maintenanceClientFactory()
	if err == nil {
		payload := fmt.Sprintf(`{"deferred":%t,"expected_updated_at":%q}`, deferred, item.UpdatedAt.Format(time.RFC3339Nano))
		err = client.do(context.Background(), "PUT", fmt.Sprintf("/api/hera/roles/%d/board/%d/defer", item.RoleID, item.ID), strings.NewReader(payload), nil)
	}
	uxlog.Log("[board] human defer role=%d item=%d deferred=%v err=%v", item.RoleID, item.ID, deferred, err)
	a.tapp.QueueUpdateDraw(func() {
		if err != nil {
			a.statusbar.SetError("Board defer failed: " + err.Error())
		}
		a.heraPage.Refresh()
	})
}
