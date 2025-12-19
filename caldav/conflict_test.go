package caldav

import (
	"testing"
	"time"

	"github.com/emersion/go-ical"
)

func TestLastModifiedWinsResolver_Resolve(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	twoHoursAgo := now.Add(-2 * time.Hour)

	tests := []struct {
		name     string
		local    *CalendarObject
		remote   *CalendarObject
		expected ConflictDecision
	}{
		{
			name: "local_newer",
			local: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: now,
				ETag:    "etag1",
			},
			remote: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: oneHourAgo,
				ETag:    "etag2",
			},
			expected: UseLocal,
		},
		{
			name: "remote_newer",
			local: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: twoHoursAgo,
				ETag:    "etag1",
			},
			remote: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: now,
				ETag:    "etag2",
			},
			expected: UseRemote,
		},
		{
			name: "equal_times_prefer_local",
			local: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: now,
				ETag:    "etag1",
			},
			remote: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: now,
				ETag:    "etag2",
			},
			expected: UseLocal,
		},
		{
			name:     "both_nil",
			local:    nil,
			remote:   nil,
			expected: Skip,
		},
		{
			name:  "local_nil",
			local: nil,
			remote: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: now,
				ETag:    "etag1",
			},
			expected: UseRemote,
		},
		{
			name: "remote_nil",
			local: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: now,
				ETag:    "etag1",
			},
			remote:   nil,
			expected: UseLocal,
		},
	}

	resolver := &LastModifiedWinsResolver{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolver.Resolve(tt.local, tt.remote)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestAlwaysUseLocalResolver_Resolve(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		local    *CalendarObject
		remote   *CalendarObject
		expected ConflictDecision
	}{
		{
			name: "both_present",
			local: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: now,
				ETag:    "etag1",
			},
			remote: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: now.Add(-1 * time.Hour),
				ETag:    "etag2",
			},
			expected: UseLocal,
		},
		{
			name:  "local_nil",
			local: nil,
			remote: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: now,
				ETag:    "etag1",
			},
			expected: UseRemote,
		},
		{
			name: "remote_nil",
			local: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: now,
				ETag:    "etag1",
			},
			remote:   nil,
			expected: UseLocal,
		},
	}

	resolver := &AlwaysUseLocalResolver{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolver.Resolve(tt.local, tt.remote)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestAlwaysUseRemoteResolver_Resolve(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		local    *CalendarObject
		remote   *CalendarObject
		expected ConflictDecision
	}{
		{
			name: "both_present",
			local: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: now,
				ETag:    "etag1",
			},
			remote: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: now.Add(-1 * time.Hour),
				ETag:    "etag2",
			},
			expected: UseRemote,
		},
		{
			name:  "local_nil",
			local: nil,
			remote: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: now,
				ETag:    "etag1",
			},
			expected: UseRemote,
		},
		{
			name: "remote_nil",
			local: &CalendarObject{
				Path:    "/cal/event1.ics",
				ModTime: now,
				ETag:    "etag1",
			},
			remote:   nil,
			expected: UseLocal,
		},
	}

	resolver := &AlwaysUseRemoteResolver{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolver.Resolve(tt.local, tt.remote)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestConflictDecision_String(t *testing.T) {
	tests := []struct {
		decision ConflictDecision
		expected string
	}{
		{UseLocal, "use_local"},
		{UseRemote, "use_remote"},
		{Merge, "merge"},
		{Skip, "skip"},
		{ConflictDecision(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.decision.String()
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestConflictResolverInterface(t *testing.T) {
	now := time.Now()
	local := &CalendarObject{
		Path:    "/cal/event1.ics",
		ModTime: now,
		Data:    &ical.Calendar{},
	}
	remote := &CalendarObject{
		Path:    "/cal/event1.ics",
		ModTime: now.Add(-1 * time.Hour),
		Data:    &ical.Calendar{},
	}

	resolvers := []struct {
		name     string
		resolver ConflictResolver
		expected ConflictDecision
	}{
		{"LastModifiedWins", &LastModifiedWinsResolver{}, UseLocal},
		{"AlwaysUseLocal", &AlwaysUseLocalResolver{}, UseLocal},
		{"AlwaysUseRemote", &AlwaysUseRemoteResolver{}, UseRemote},
	}

	for _, tt := range resolvers {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.resolver.Resolve(local, remote)
			if result != tt.expected {
				t.Errorf("%s: expected %s, got %s", tt.name, tt.expected, result)
			}
		})
	}
}
