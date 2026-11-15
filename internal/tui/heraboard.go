package tui

import (
	"fmt"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/uxlog"
)

func (a *App) confirmResidentApproval(d *db.DB, item *db.HeraBoardItem, approve bool) {
	action := "Deny"
	message := fmt.Sprintf("Deny the exact call shown on board #%d? Solo will be told to continue other work.", item.ID)
	if approve {
		action = "Approve once"
		message = fmt.Sprintf("Approve the exact call shown on board #%d once? The grant expires in ten minutes. A changed launch requires a new request.", item.ID)
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
					a.statusbar.SetError("Approval failed: " + err.Error())
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
