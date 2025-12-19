package caldav

import (
	"testing"
	"time"

	"github.com/emersion/go-webdav"
)

func TestQueryOptions(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)
	token := "sync-token-123"

	opts := &QueryOptions{
		TimeRangeStart: &start,
		TimeRangeEnd:   &end,
		SyncToken:      token,
	}

	if opts.TimeRangeStart == nil || opts.TimeRangeEnd == nil {
		t.Fatal("QueryOptions should have both start and end times")
	}

	if !opts.TimeRangeStart.Equal(start) {
		t.Errorf("Expected start time %v, got %v", start, opts.TimeRangeStart)
	}

	if !opts.TimeRangeEnd.Equal(end) {
		t.Errorf("Expected end time %v, got %v", end, opts.TimeRangeEnd)
	}

	if opts.SyncToken != token {
		t.Errorf("Expected sync token %q, got %q", token, opts.SyncToken)
	}
}

func TestPutOptions(t *testing.T) {
	etag := "\"abc123\""

	opts := &PutOptions{
		IfMatch:     webdav.ConditionalMatch(etag),
		IfNoneMatch: webdav.ConditionalMatch("*"),
	}

	expectedMatch := webdav.ConditionalMatch(etag)
	if opts.IfMatch != expectedMatch {
		t.Errorf("Expected If-Match %q, got %q", expectedMatch, opts.IfMatch)
	}

	expectedNoneMatch := webdav.ConditionalMatch("*")
	if opts.IfNoneMatch != expectedNoneMatch {
		t.Errorf("Expected If-None-Match %q, got %q", expectedNoneMatch, opts.IfNoneMatch)
	}
}

func TestSyncResult(t *testing.T) {
	result := NewSyncResult("token-123")

	if result.SyncToken != "token-123" {
		t.Errorf("Expected sync token 'token-123', got %q", result.SyncToken)
	}

	if len(result.Created) != 0 || len(result.Updated) != 0 || len(result.Deleted) != 0 {
		t.Error("New sync result should have empty slices")
	}

	// Test AddCreated
	obj := CalendarObject{Path: "/test"}
	result.AddCreated(obj)
	if len(result.Created) != 1 {
		t.Error("Should have 1 created object")
	}

	// Test AddUpdated
	result.AddUpdated(obj)
	if len(result.Updated) != 1 {
		t.Error("Should have 1 updated object")
	}

	// Test AddDeleted
	result.AddDeleted("/deleted", "uid-123")
	if len(result.Deleted) != 1 {
		t.Error("Should have 1 deleted object")
	}
	if result.Deleted[0].Path != "/deleted" || result.Deleted[0].UID != "uid-123" {
		t.Error("Deleted object should have correct path and UID")
	}

	// Test TotalChanges
	if result.TotalChanges() != 3 {
		t.Errorf("Expected 3 total changes, got %d", result.TotalChanges())
	}
}

func TestExtractUIDFromPath(t *testing.T) {
	client := &Client{} // Create client to access private method via reflection or test

	testCases := []struct {
		path     string
		expected string
	}{
		{"/calendars/user/event-123.ics", "event-123"},
		{"/event-456.ics", "event-456"},
		{"/path/to/event-789", "event-789"},
		{"/no-extension", "no-extension"},
		{"", ""},
	}

	for _, tc := range testCases {
		result := client.extractUIDFromPath(tc.path)
		if result != tc.expected {
			t.Errorf("extractUIDFromPath(%q) = %q, expected %q", tc.path, result, tc.expected)
		}
	}
}

func TestSetConflictResolver(t *testing.T) {
	client := &Client{}

	if client.conflictResolver != nil {
		t.Error("Expected conflictResolver to be nil by default")
	}

	resolver := &LastModifiedWinsResolver{}
	client.SetConflictResolver(resolver)

	if client.conflictResolver != resolver {
		t.Error("Expected conflictResolver to be set")
	}

	client.SetConflictResolver(nil)
	if client.conflictResolver != nil {
		t.Error("Expected conflictResolver to be nil after setting to nil")
	}
}

func TestConflictDecisionString(t *testing.T) {
	testCases := []struct {
		decision ConflictDecision
		expected string
	}{
		{UseLocal, "use_local"},
		{UseRemote, "use_remote"},
		{Merge, "merge"},
		{Skip, "skip"},
		{ConflictDecision(999), "unknown"},
	}

	for _, tc := range testCases {
		result := tc.decision.String()
		if result != tc.expected {
			t.Errorf("ConflictDecision(%d).String() = %q, expected %q", tc.decision, result, tc.expected)
		}
	}
}

func TestQueryOptions_Nil(t *testing.T) {
	var opts *QueryOptions

	if opts != nil {
		t.Error("Nil QueryOptions should be nil")
	}
}

func TestQueryOptions_Empty(t *testing.T) {
	opts := &QueryOptions{}

	if opts.TimeRangeStart != nil {
		t.Error("Empty QueryOptions should have nil TimeRangeStart")
	}

	if opts.TimeRangeEnd != nil {
		t.Error("Empty QueryOptions should have nil TimeRangeEnd")
	}

	if opts.SyncToken != "" {
		t.Error("Empty QueryOptions should have empty SyncToken")
	}
}

func TestPutOptions_Nil(t *testing.T) {
	var opts *PutOptions

	if opts != nil {
		t.Error("Nil PutOptions should be nil")
	}
}

func TestPutOptions_Empty(t *testing.T) {
	opts := &PutOptions{}

	if opts.IfMatch != "" {
		t.Error("Empty PutOptions should have empty IfMatch")
	}

	if opts.IfNoneMatch != "" {
		t.Error("Empty PutOptions should have empty IfNoneMatch")
	}
}

func TestSyncDeletedItem_ExtractUID(t *testing.T) {
	testCases := []struct {
		name     string
		path     string
		uid      string
		expected string
	}{
		{
			name:     "explicit_uid",
			path:     "/calendars/user/event.ics",
			uid:      "explicit-uid-123",
			expected: "explicit-uid-123",
		},
		{
			name:     "extract_from_path_with_ics",
			path:     "/calendars/user/my-event-456.ics",
			uid:      "",
			expected: "my-event-456",
		},
		{
			name:     "extract_from_path_without_extension",
			path:     "/calendars/user/event-789",
			uid:      "",
			expected: "event-789",
		},
		{
			name:     "empty_both",
			path:     "",
			uid:      "",
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			item := SyncDeletedItem{
				Path: tc.path,
				UID:  tc.uid,
			}

			result := item.ExtractUID()
			if result != tc.expected {
				t.Errorf("Expected UID %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestSyncResult_Empty(t *testing.T) {
	result := NewSyncResult("")

	if result.SyncToken != "" {
		t.Errorf("Expected empty sync token, got %q", result.SyncToken)
	}

	if result.TotalChanges() != 0 {
		t.Errorf("Expected 0 total changes, got %d", result.TotalChanges())
	}
}

func TestSyncResult_MultipleOperations(t *testing.T) {
	result := NewSyncResult("test-token")

	result.AddCreated(CalendarObject{Path: "/cal/event1.ics"})
	result.AddCreated(CalendarObject{Path: "/cal/event2.ics"})
	result.AddUpdated(CalendarObject{Path: "/cal/event3.ics"})
	result.AddDeleted("/cal/event4.ics", "uid-4")
	result.AddDeleted("/cal/event5.ics", "uid-5")
	result.AddDeleted("/cal/event6.ics", "uid-6")

	if len(result.Created) != 2 {
		t.Errorf("Expected 2 created objects, got %d", len(result.Created))
	}

	if len(result.Updated) != 1 {
		t.Errorf("Expected 1 updated object, got %d", len(result.Updated))
	}

	if len(result.Deleted) != 3 {
		t.Errorf("Expected 3 deleted objects, got %d", len(result.Deleted))
	}

	if result.TotalChanges() != 6 {
		t.Errorf("Expected 6 total changes, got %d", result.TotalChanges())
	}
}
