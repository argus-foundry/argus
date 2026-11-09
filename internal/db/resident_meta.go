package db

import (
	"time"
)

// Resident reply/unread state lives in task_meta under this namespace, keyed to
// the resident task row (add-resident-agent D7). The coord-hook writes
// MetaKeyLastReply/MetaKeyLastReplyAt on every resident Stop; the
// ResidentMarkViewed RPC writes MetaKeyLastViewedAt. Timestamps are
// RFC3339Nano UTC.
const (
	ResidentMetaNamespace = "resident"
	MetaKeyLastReply      = "last_reply"
	MetaKeyLastReplyAt    = "last_reply_at"
	MetaKeyLastViewedAt   = "last_viewed_at"
)

// ResidentTimeFormat is the layout of the resident's *_at meta values.
const ResidentTimeFormat = time.RFC3339Nano
