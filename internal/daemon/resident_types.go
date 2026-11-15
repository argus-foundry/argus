package daemon

// ResidentStateResp is daemon-owned frontend state, outside the supervisor's
// wire manifest. Reason describes an unmet prerequisite when enabled but down.
type ResidentStateResp struct {
	TimerCount     int
	NextTimerAt    string
	RunningWorkers int
	Enabled        bool
	Name           string
	TaskID         string
	Running        bool
	Unread         bool
	Waiting        bool
	LastReply      string
	LastReplyAt    string // RFC3339Nano UTC, empty when none
	ViewedAt       string // RFC3339Nano UTC, empty when never viewed
	Reason         string
}
