package hera

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/argus-foundry/argus/internal/app/agentview"
	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/tui/keymap"
	"github.com/argus-foundry/argus/internal/tui/theme"
	"github.com/argus-foundry/argus/internal/tui/widget"
	"github.com/argus-foundry/argus/internal/uxlog"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
	"github.com/rivo/uniseg"
)

type boardView struct {
	allItems     []*db.HeraBoardItem
	tab          int
	items        []*db.HeraBoardItem
	cursor       int
	offset       int
	expandedID   int64
	followCursor bool
	rect         widget.InnerRect
}

func newBoardView() *boardView { return &boardView{tab: 4} }

func (p *HeraPage) SetBoardAuto(enabled bool) {
	p.boardAuto = enabled
	p.rail.SetBoardAuto(enabled)
}

// BoardActions reports the board commands applicable to the current region.
// Physical dispatch stays in InputHandler so palette actions obey identical
// focus, filtering, and PTY forwarding rules.
func (p *HeraPage) BoardActions() []keymap.Action {
	if !p.detailsMode || p.rail.Filtering() {
		return nil
	}
	switch p.focus.State() {
	case FocusRail:
		return []keymap.Action{keymap.ActBoardToggle}
	case FocusAgent:
		if p.showBoard {
			return keymap.ContextOrder(keymap.CtxHeraBoard)
		}
		return []keymap.Action{keymap.ActBoardToggle}
	default:
		return nil
	}
}

func (r *Rail) coordMarker(orchID int64) rune {
	if r.boardAuto {
		if o := r.model.OrchByID(orchID); o != nil {
			if c := o.CoordRole(); c != nil && c.BoardUnread > 0 {
				return '\U000f01ee'
			} // nf-md-email
		}
	}
	return heraIconCoord
}

func (p *HeraPage) updateBoard(o *heramodel.OrchView) {
	if o == nil {
		p.boardOrchID = 0
		p.showBoard = false
		p.board = newBoardView()
		return
	}
	c := o.CoordRole()
	if p.boardOrchID != o.ID {
		p.board = newBoardView()
		p.showBoard = p.boardAuto && c != nil && c.BoardUnread > 0
		p.boardOrchID = o.ID
		uxlog.Log("[board] selection orch=%d board=%v", o.ID, p.showBoard)
	}
	var selected int64
	if i := p.board.selected(); i != nil {
		selected = i.ID
	}
	// Only newly unread versions trigger auto-show. A manual switch to the
	// plan must survive ordinary refreshes of the same open reminders.
	if p.residentMode && p.boardAuto && !p.showBoard && c != nil {
		for _, item := range c.BoardItems {
			if !heramodel.BoardAttention(item) {
				continue
			}
			known := false
			for _, old := range p.board.sourceItems() {
				if old.ID == item.ID && !old.Read && old.UpdatedAt.Equal(item.UpdatedAt) {
					known = true
					break
				}
			}
			if !known {
				p.showBoard = true
				uxlog.Log("[board] unread arrival orch=%d item=%d resident=%v", o.ID, item.ID, p.residentMode)
				break
			}
		}
	}
	p.board.allItems = []*db.HeraBoardItem{}
	if c != nil {
		// The model already sorts; sorting again keeps injected/remote models
		// in the shared order (blocking, ordinary, quiet) at no cost.
		heramodel.SortBoardItems(c.BoardItems)
		for _, item := range c.BoardItems {
			if !item.Resolved {
				p.board.allItems = append(p.board.allItems, item)
			}
		}
	}
	if p.boardTabs == nil {
		p.boardTabs = make(map[int64]int)
	}
	if tab, ok := p.boardTabs[o.ID]; ok {
		p.board.tab = tab
	} else {
		p.board.tab = 4
		counts := p.board.counts()
		for n, count := range counts[:4] {
			if count > 0 {
				p.board.tab = n
				break
			}
		}
		p.boardTabs[o.ID] = p.board.tab
	}
	expandedID := p.board.expandedID
	p.board.filter(selected)
	if expandedID != 0 && p.board.expandedID == 0 {
		uxlog.Log("[board] expansion cleared orch=%d item=%d: no longer visible", o.ID, expandedID)
	}
}

func (b *boardView) selected() *db.HeraBoardItem {
	if b.cursor < 0 || b.cursor >= len(b.items) {
		return nil
	}
	return b.items[b.cursor]
}

func boardText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
}

func (p *HeraPage) boardTitle() string {
	if !p.boardAuto && !p.showBoard {
		return " Plan "
	}
	unread := 0
	for _, i := range p.board.sourceItems() {
		if heramodel.BoardAttention(i) {
			unread++
		}
	}
	return fmt.Sprintf(" Plan · Board (%d) ", unread)
}

func boardHint(keys *keymap.Keymap, focused bool) string {
	label := func(act keymap.Action) string {
		binding, _ := keys.BindingFor(keymap.CtxHeraBoard, act)
		if binding.String() == "left" {
			return "←"
		}
		if binding.String() == "right" {
			return "→"
		}
		return binding.String()
	}
	if !focused {
		return "⌘→ open board · " + label(keymap.ActBoardToggle) + " plan"
	}
	return label(keymap.ActBoardTabPrev) + "/" + label(keymap.ActBoardTabNext) + " tabs · " + fmt.Sprintf("%s/%s move · ⏎ expand · %s reply · %s dismiss/drop · %s done · %s copy · %s decide · %s plan · ctrl+q back",
		label(keymap.ActBoardDown), label(keymap.ActBoardUp), label(keymap.ActBoardReply),
		label(keymap.ActBoardDismiss), label(keymap.ActBoardDone), label(keymap.ActBoardCopy),
		label(keymap.ActBoardDecide), label(keymap.ActBoardToggle))
}

type boardLine struct {
	text           string
	bold, selected bool
	boldRunes      []bool
	// tone colors a header: "blocking" stands out, "quiet" dims deferred work.
	tone string
}

// boardBold removes only paired, nonempty bold markers. Other syntax stays literal.
func boardBold(s string) (string, []bool) {
	var text strings.Builder
	var bold []bool
	appendText := func(s string, strong bool) {
		text.WriteString(s)
		for range s {
			bold = append(bold, strong)
		}
	}
	for len(s) > 0 {
		start := strings.Index(s, "**")
		if start < 0 {
			appendText(s, false)
			break
		}
		end := strings.Index(s[start+2:], "**")
		if end <= 0 {
			appendText(s[:start+2], false)
			s = s[start+2:]
			continue
		}
		appendText(s[:start], false)
		appendText(s[start+2:start+2+end], true)
		s = s[start+4+end:]
	}
	return text.String(), bold
}

// boardListPrefixLen returns the byte length of a list prefix: optional leading
// spaces (nesting, one level per two spaces) then "- ", "* " or "N. ". Zero
// means the line is not a list item and stays literal.
func boardListPrefixLen(line string) int {
	spaces := len(line) - len(strings.TrimLeft(line, " "))
	rest := line[spaces:]
	switch {
	case strings.HasPrefix(rest, "- "), strings.HasPrefix(rest, "* "):
		return spaces + 2
	}
	digits := 0
	for digits < len(rest) && digits < 9 && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits > 0 && strings.HasPrefix(rest[digits:], ". ") {
		return spaces + digits + 2
	}
	return 0
}

func boardBodyLines(body string, width int) []boardLine {
	var lines []boardLine
	indent := strings.Repeat(" ", min(2, max(0, width-1)))
	body, allBold := boardBold(strings.TrimRight(boardText(body), "\n"))
	offset := 0
	for _, paragraph := range strings.Split(body, "\n") {
		length := utf8.RuneCountInString(paragraph)
		bold := allBold[offset : offset+length]
		offset += length + 1
		bullet := ""
		if n := boardListPrefixLen(paragraph); n > 0 {
			bullet, paragraph = paragraph[:n], paragraph[n:]
			bold = bold[n:]
		}
		source := []rune(paragraph)
		pos := 0
		padding := indent + strings.Repeat(" ", min(len(bullet), max(0, width-len(indent)-1)))
		for n, wrapped := range widget.WrapPreservingLines(paragraph, max(1, width-len(padding))) {
			prefix := padding
			if n == 0 && bullet != "" {
				prefix = indent + bullet[:len(padding)-len(indent)]
			}
			line := boardLine{text: prefix + wrapped, boldRunes: make([]bool, len(prefix))}
			// The shared wrapper normalizes whitespace; consume matching source
			// runes so styles survive word wraps and hard-broken tokens alike.
			for _, r := range wrapped {
				if r != ' ' {
					for pos < len(source) && source[pos] != r {
						pos++
					}
				}
				strong := pos < len(bold) && bold[pos]
				line.boldRunes = append(line.boldRunes, strong)
				if r == ' ' {
					for pos < len(source) && unicode.IsSpace(source[pos]) {
						pos++
					}
				} else if pos < len(source) {
					pos++
				}
			}
			lines = append(lines, line)
		}
	}
	return lines
}

func (l boardLine) draw(screen tcell.Screen, x, y, width int, focused bool) {
	col, index := 0, 0
	graphemes := uniseg.NewGraphemes(l.text)
	for graphemes.Next() {
		runes := graphemes.Runes()
		w := runewidth.StringWidth(graphemes.Str())
		if col+w > width {
			break
		}
		base := theme.StyleNormal
		switch l.tone {
		case boardToneBlocking:
			base = theme.StyleNeedsInput
		case boardToneQuiet:
			base = theme.StyleDimmed
		}
		style := base.Bold(l.bold || (index < len(l.boldRunes) && l.boldRunes[index])).Reverse(l.selected && focused)
		if w > 0 {
			screen.SetContent(x+col, y, runes[0], runes[1:], style)
		}
		col += w
		index += len(runes)
	}
}

// Approval input is literal data: Markdown markers and repeated spaces are
// meaningful tool arguments, so the ordinary board Markdown wrapper is unsafe.
func approvalBodyLines(body string, width int) []boardLine {
	var safe strings.Builder
	for _, r := range body {
		if unicode.Is(unicode.Cf, r) || (unicode.IsControl(r) && r != '\n') {
			fmt.Fprintf(&safe, "\\u%04x", r)
		} else {
			safe.WriteRune(r)
		}
	}
	var lines []boardLine
	for _, paragraph := range strings.Split(safe.String(), "\n") {
		line, col := "", 0
		g := uniseg.NewGraphemes(paragraph)
		for g.Next() {
			w := runewidth.StringWidth(g.Str())
			if col+w > max(1, width-2) && line != "" {
				lines = append(lines, boardLine{text: "  " + line})
				line, col = "", 0
			}
			line += g.Str()
			col += w
		}
		lines = append(lines, boardLine{text: "  " + line})
	}
	return lines
}

func (b *boardView) draw(screen tcell.Screen, x, y, w, h int, title string, focused bool, keys *keymap.Keymap) {
	b.rect = widget.InnerRect{X: x, Y: y, W: w, H: h}
	border := theme.StyleBorder
	if focused {
		border = theme.StyleFocusedBorder
	}
	inner := widget.DrawBorderedPanel(screen, x, y, w, h, title, border)
	if inner.W <= 0 || inner.H <= 0 {
		b.offset = 0
		return
	}
	// Reserve the footer before layout and scrolling, including empty boards.
	hintText := boardHint(keys, focused)
	if item := b.selected(); focused && item != nil && item.ResidentApproval {
		if item.ApprovalState != "" && item.ApprovalState != db.ApprovalPending {
			hintText = approvalStatusLine(item) + " · ⏎ expand · PgUp/PgDn scroll"
		} else {
			reply, _ := keys.BindingFor(keymap.CtxHeraBoard, keymap.ActBoardReply)
			dismiss, _ := keys.BindingFor(keymap.CtxHeraBoard, keymap.ActBoardDismiss)
			hintText = fmt.Sprintf("%s approve once · %s deny · ⏎ expand · PgUp/PgDn scroll", reply.String(), dismiss.String())
		}
	}
	hint := runewidth.Truncate(hintText, inner.W, "")
	tview.Print(screen, tview.Escape(hint), inner.X, inner.Y+inner.H-1, inner.W, tview.AlignLeft, theme.ColorDimmed)
	inner.H--
	if inner.H == 0 {
		b.offset = 0
		return
	}
	b.drawTabs(screen, inner.X, inner.Y, inner.W)
	inner.Y++
	inner.H--
	if inner.H == 0 {
		b.offset = 0
		return
	}
	var lines []boardLine
	selectedStart := 0
	for n, i := range b.items {
		selected := n == b.cursor
		if selected {
			selectedStart = len(lines)
		}
		marker := "▸"
		if b.expandedID == i.ID {
			marker = "▾"
		}
		unread := ""
		if !i.Read {
			unread = " •"
			if inner.W == 1 {
				unread = "•"
			}
		}
		prefix := boardText(fmt.Sprintf("%s [%s] #%d ", marker, boardKindLabel(i), i.ID))
		subject := boardText(i.Subject)
		tone := boardTone(i)
		if b.expandedID == i.ID {
			// Leave room for a wide subject rune even in very narrow panels.
			prefix = runewidth.Truncate(prefix, max(0, inner.W-runewidth.StringWidth(unread)-2), "")
			indent := strings.Repeat(" ", runewidth.StringWidth(prefix))
			markerOnly := unread != "" && inner.W-runewidth.StringWidth(unread) < 2
			wrapWidth := max(1, inner.W-len(indent)-runewidth.StringWidth(unread))
			if markerOnly {
				lines = append(lines, boardLine{text: unread, bold: true, selected: selected, tone: tone})
				wrapWidth = inner.W
			}
			for n, wrapped := range widget.WrapPreservingLines(subject, wrapWidth) {
				header := indent + wrapped
				if n == 0 && !markerOnly {
					header = prefix + wrapped + unread
				}
				lines = append(lines, boardLine{text: header, bold: true, selected: selected && n == 0 && !markerOnly, tone: tone})
			}
			if i.ResidentApproval {
				lines = append(lines, approvalBodyLines(i.Body, inner.W)...)
			} else {
				lines = append(lines, boardBodyLines(boardItemBody(i), inner.W)...)
				lines = append(lines, boardMetaLines(i, inner.W)...)
			}
		} else {
			header := runewidth.Truncate(prefix+subject, max(0, inner.W-runewidth.StringWidth(unread)), "…") + unread
			if inner.W <= runewidth.StringWidth(unread) {
				header = unread
			}
			lines = append(lines, boardLine{text: header, bold: true, selected: selected, tone: tone})
		}
	}
	if len(lines) == 0 {
		b.offset = 0
		widget.DrawText(screen, inner.X, inner.Y, inner.W, "No open board items", theme.StyleDimmed)
		return
	}
	if b.followCursor {
		if selectedStart < b.offset {
			b.offset = selectedStart
		}
		if selectedStart >= b.offset+inner.H {
			b.offset = selectedStart - inner.H + 1
		}
		b.followCursor = false
	}
	b.offset = max(0, min(b.offset, max(0, len(lines)-inner.H)))
	for n := b.offset; n < len(lines) && n-b.offset < inner.H; n++ {
		lines[n].draw(screen, inner.X, inner.Y+n-b.offset, inner.W, focused)
	}
}

func (b *boardView) handleMouse(action tview.MouseAction, event *tcell.EventMouse) bool {
	x, y := event.Position()
	if x < b.rect.X || x >= b.rect.X+b.rect.W || y < b.rect.Y || y >= b.rect.Y+b.rect.H {
		return false
	}
	switch action {
	case tview.MouseScrollDown:
		b.offset += 3
	case tview.MouseScrollUp:
		b.offset -= 3
	default:
		return false
	}
	// Draw clamps against freshly wrapped content. Explicit scrolling must not
	// snap back to a header merely because cursor follow was pending.
	b.followCursor = false
	return true
}

func (p *HeraPage) handleBoardKey(event *tcell.EventKey, setFocus func(tview.Primitive)) {
	act, _ := p.keys().Resolve(keymap.CtxHeraBoard, event)
	switch {
	case act == keymap.ActBoardTabPrev:
		p.changeBoardTab(p.board.tab - 1)
	case act == keymap.ActBoardTabNext:
		p.changeBoardTab(p.board.tab + 1)
	case act == keymap.ActBoardDecide:
		if p.IsRemote() {
			return
		}
		var role, focus int64
		if o := p.rail.model.OrchByID(p.boardOrchID); o != nil {
			if c := o.CoordRole(); c != nil {
				role = c.RoleID
			}
		}
		if i := p.board.selected(); i != nil {
			role = i.RoleID
			if !i.Resolved {
				focus = i.ID
			}
		}
		uxlog.Log("[board] open attempt role=%d focus=%d", role, focus)
		if p.OnBoardDecide != nil {
			p.OnBoardDecide(role, focus)
		}
	case event.Key() == tcell.KeyDown || act == keymap.ActBoardDown:
		p.board.cursor = min(len(p.board.items)-1, p.board.cursor+1)
		p.board.followCursor = true
	case event.Key() == tcell.KeyUp || act == keymap.ActBoardUp:
		p.board.cursor = max(0, p.board.cursor-1)
		p.board.followCursor = true
	case event.Key() == tcell.KeyPgDn:
		p.board.offset += max(1, p.boardHeight-4)
	case event.Key() == tcell.KeyPgUp:
		p.board.offset = max(0, p.board.offset-max(1, p.boardHeight-4))
	default:
		i := p.board.selected()
		if i == nil {
			return
		}
		switch {
		case event.Key() == tcell.KeyEnter:
			p.board.followCursor = true
			if p.board.expandedID == i.ID {
				p.board.expandedID = 0
			} else {
				p.board.expandedID = i.ID
				if p.OnBoardRead != nil {
					p.OnBoardRead(i.RoleID, i.ID, i.UpdatedAt)
				}
			}
			uxlog.Log("[board] expansion orch=%d item=%d expanded=%v", p.boardOrchID, i.ID, p.board.expandedID == i.ID)
		case act == keymap.ActBoardReply:
			if i.ResidentApproval {
				p.decideBoardApproval(i, true)
			} else {
				p.replyToBoard(i)
			}
		case act == keymap.ActBoardDismiss:
			switch {
			case i.ResidentApproval:
				p.decideBoardApproval(i, false)
			case i.Kind == "todo":
				p.completeBoardItem(i, "dropped")
			case i.Kind == "action":
				uxlog.Log("[board] drop skipped item=%d: actions complete with done", i.ID)
				p.info("An action can only be marked done")
			case p.OnBoardDismiss != nil:
				p.OnBoardDismiss(i.RoleID, i.ID)
			}
		case act == keymap.ActBoardDone:
			p.completeBoardItem(i, "done")
		case act == keymap.ActBoardCopy:
			p.copyBoardCommand(i)
		}
	}
}

func (p *HeraPage) decideBoardApproval(i *db.HeraBoardItem, approve bool) {
	// A decided or lapsed item never reopens the approve/deny dialog; say
	// what already happened instead.
	if i.ApprovalState != "" && i.ApprovalState != db.ApprovalPending {
		if p.OnInfo != nil {
			p.OnInfo(approvalStatusLine(i))
		}
		return
	}
	if p.board.expandedID != i.ID {
		p.board.expandedID = i.ID
		if p.OnBoardRead != nil {
			p.OnBoardRead(i.RoleID, i.ID, i.UpdatedAt)
		}
		if p.OnInfo != nil {
			p.OnInfo("Review the exact call, then reply to approve once or dismiss to deny")
		}
		return
	}
	if p.OnApprovalDecision != nil {
		p.OnApprovalDecision(i, approve)
	}
}

// approvalStatusLine summarizes a decided approval item from its stored
// state and resolution note (which carries the decision times).
func approvalStatusLine(i *db.HeraBoardItem) string {
	line := fmt.Sprintf("#%d %s", i.ID, i.ApprovalState)
	if i.ResolutionNote != "" {
		line += ": " + i.ResolutionNote
	}
	return line
}

func (p *HeraPage) replyToBoard(i *db.HeraBoardItem) {
	var sess agentview.TerminalAdapter
	if p.resolve != nil {
		sess = p.resolve(p.coordBound)
	}
	if sess == nil || !sess.Alive() {
		uxlog.Log("[board] reply skipped role=%d item=%d: no live coordinator", i.RoleID, i.ID)
		if p.OnInfo != nil {
			p.OnInfo("Restart the coordinator before replying")
		}
		return
	}
	data := []byte(fmt.Sprintf("\x1b[200~Re: %s (#%d)\n\n\x1b[201~", i.Subject, i.ID))
	n, err := sess.WriteInput(data, agentview.OriginUser)
	if err != nil || n != len(data) {
		uxlog.Log("[board] reply failed role=%d item=%d bytes=%d/%d: %v", i.RoleID, i.ID, n, len(data), err)
		if p.OnInfo != nil {
			p.OnInfo("Board reply paste failed; check the coordinator composer")
		}
		return
	}
	p.coordPane.ResetScroll()
	p.focus.SetRegion(FocusCoord)
	uxlog.Log("[board] reply composed role=%d item=%d", i.RoleID, i.ID)
}

func boardItemBody(i *db.HeraBoardItem) string {
	if i.Structured == nil {
		return i.Body
	}
	var b strings.Builder
	b.WriteString(i.Body)
	b.WriteString("\n\n**Options**\n\n")
	for _, o := range i.Structured.Options {
		fmt.Fprintf(&b, "- **%s**", o.Label)
		if o.ID == i.Structured.Recommended {
			b.WriteString(" (recommended)")
		}
		b.WriteString("\n")
		if o.Summary != "" {
			fmt.Fprintf(&b, "  - %s\n", o.Summary)
		}
		fmt.Fprintf(&b, "  - Upside: %s\n  - Downside: %s\n", o.Upside, o.Downside)
	}
	if i.Structured.Reason != "" {
		fmt.Fprintf(&b, "\n**Recommendation:** %s", i.Structured.Reason)
	}
	return b.String()
}

const (
	boardToneBlocking = "blocking"
	boardToneQuiet    = "quiet"
)

// boardKindLabel names the item's kind, marking blocking actions and deferred
// (trigger-pending) work so the state is readable without expanding the row.
func boardKindLabel(i *db.HeraBoardItem) string {
	switch {
	case i.Kind == "action" && i.Blocking:
		return "action BLOCKING"
	case (i.Kind == "action" || i.Kind == "todo") && i.TriggerPending:
		return i.Kind + " deferred"
	}
	return i.Kind
}

func boardTone(i *db.HeraBoardItem) string {
	switch {
	case i.Kind == "action" && i.Blocking:
		return boardToneBlocking
	case heramodel.BoardQuiet(i):
		return boardToneQuiet
	}
	return ""
}

// boardMetaLines renders trigger/readiness and the exact step for action and
// todo items. The command is literal data (never Markdown-parsed) so the text
// shown is the text Copy puts on the clipboard.
func boardMetaLines(i *db.HeraBoardItem, width int) []boardLine {
	if i.Kind != "action" && i.Kind != "todo" {
		return nil
	}
	var lines []boardLine
	if t := strings.TrimSpace(i.Trigger); t != "" {
		state := "ready"
		if i.TriggerPending {
			state = "deferred"
		}
		lines = append(lines, boardBodyLines(fmt.Sprintf("**Trigger (%s):** %s", state, t), width)...)
	}
	if i.Command != "" {
		lines = append(lines, boardLine{text: "  Step:", bold: true})
		lines = append(lines, approvalBodyLines(i.Command, width)...)
	}
	return lines
}

func (p *HeraPage) info(msg string) {
	if p.OnInfo != nil {
		p.OnInfo(msg)
	}
}

// completeBoardItem requests Done (action/todo) or Dropped (todo) for the
// selected item at its visible revision. Pending items stay completable: Aaron
// may finish deferred work early.
func (p *HeraPage) completeBoardItem(i *db.HeraBoardItem, outcome string) {
	if (i.Kind != "action" && i.Kind != "todo") || i.Resolved {
		uxlog.Log("[board] complete skipped item=%d kind=%s: not an open action/todo", i.ID, i.Kind)
		p.info("Done applies to open actions and todos")
		return
	}
	if p.OnBoardComplete == nil {
		uxlog.Log("[board] complete skipped item=%d: no handler (remote)", i.ID)
		return
	}
	p.OnBoardComplete(i, outcome)
}

func (p *HeraPage) copyBoardCommand(i *db.HeraBoardItem) {
	if i.Kind != "action" || i.Command == "" {
		uxlog.Log("[board] copy skipped item=%d kind=%s: no step", i.ID, i.Kind)
		p.info("Nothing to copy: this item has no command or step")
		return
	}
	if p.OnBoardCopy == nil {
		return
	}
	p.OnBoardCopy(i)
}
