package caldav

// SyncResult contains incremental synchronization result (RFC 6578 sync-collection)
type SyncResult struct {
	Created   []CalendarObject  // New events
	Updated   []CalendarObject  // Modified events
	Deleted   []SyncDeletedItem // Deleted events
	SyncToken string            // New sync-token for next request
}

// SyncDeletedItem represents deleted event in sync results
type SyncDeletedItem struct {
	Path string // Path to event (e.g., "/calendar/event.ics")
	UID  string // Event UID (extracted from path if possible)
}

// ExtractUID returns UID if set, otherwise extracts it from Path
func (sdi *SyncDeletedItem) ExtractUID() string {
	if sdi.UID != "" {
		return sdi.UID
	}

	if sdi.Path == "" {
		return ""
	}

	// Extract from path (same logic as Client.extractUIDFromPath)
	// Find last slash
	lastSlash := -1
	for i := len(sdi.Path) - 1; i >= 0; i-- {
		if sdi.Path[i] == '/' {
			lastSlash = i
			break
		}
	}

	filename := sdi.Path[lastSlash+1:]

	// Remove .ics extension if present
	if len(filename) > 4 && filename[len(filename)-4:] == ".ics" {
		return filename[:len(filename)-4]
	}

	return filename
}

// NewSyncResult creates new synchronization result
func NewSyncResult(syncToken string) *SyncResult {
	return &SyncResult{
		Created:   make([]CalendarObject, 0),
		Updated:   make([]CalendarObject, 0),
		Deleted:   make([]SyncDeletedItem, 0),
		SyncToken: syncToken,
	}
}

// AddCreated adds created event
func (sr *SyncResult) AddCreated(obj CalendarObject) {
	sr.Created = append(sr.Created, obj)
}

// AddUpdated adds modified event
func (sr *SyncResult) AddUpdated(obj CalendarObject) {
	sr.Updated = append(sr.Updated, obj)
}

// AddDeleted adds deleted event
func (sr *SyncResult) AddDeleted(path, uid string) {
	sr.Deleted = append(sr.Deleted, SyncDeletedItem{
		Path: path,
		UID:  uid,
	})
}

// TotalChanges returns total number of changes
func (sr *SyncResult) TotalChanges() int {
	return len(sr.Created) + len(sr.Updated) + len(sr.Deleted)
}
