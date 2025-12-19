package caldav

import "errors"

// CalDAV-specific errors
var (
	// ErrSyncTokenExpired returned when sync-token expired (HTTP 410 Gone)
	ErrSyncTokenExpired = errors.New("caldav: sync token expired (HTTP 410)")

	// ErrPreconditionFailed returned when preconditions failed (HTTP 412 Precondition Failed)
	ErrPreconditionFailed = errors.New("caldav: precondition failed (HTTP 412)")

	// ErrConflict returned on resource conflicts
	ErrConflict = errors.New("caldav: resource conflict")

	// ErrNotFound returned when resource not found (HTTP 404 Not Found)
	ErrNotFound = errors.New("caldav: resource not found (HTTP 404)")

	// ErrMergeNotSupported returned when merge conflict resolution is not supported
	ErrMergeNotSupported = errors.New("caldav: merge conflict resolution not supported")
)
