package caldav

import (
	"time"

	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/internal"
)

// QueryOptions contains optional parameters for QueryCalendar
type QueryOptions struct {
	TimeRangeStart *time.Time
	TimeRangeEnd   *time.Time
	SyncToken      string
}

// PutOptions contains optional parameters for PutCalendarObject
type PutOptions struct {
	IfMatch     webdav.ConditionalMatch
	IfNoneMatch webdav.ConditionalMatch
}

type SyncOptions struct {
	SyncLevel internal.Depth
}
