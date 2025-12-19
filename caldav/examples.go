package caldav

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
)

// Example demonstrates basic CalDAV operations with the enhanced client.
func Example() {
	ctx := context.Background()
	httpClient := &http.Client{} // Configure as needed
	client, err := NewClient(httpClient, "https://example.com/caldav")
	if err != nil {
		panic(err)
	}

	// Create a new calendar event
	cal := ical.NewCalendar()
	event := ical.NewEvent()
	event.Props.SetText(ical.PropUID, "example-uid")
	event.Props.SetDateTime(ical.PropDateTimeStart, time.Now())
	event.Props.SetDateTime(ical.PropDateTimeEnd, time.Now().Add(time.Hour))
	event.Props.SetText(ical.PropSummary, "Example Event")
	cal.Children = []*ical.Component{
		event.Component,
	}

	// Create event on server
	obj, err := client.PutCalendarObject(ctx, "/calendar/example.ics", cal, nil)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Created event with ETag: %s\n", obj.ETag)

	// Update event with optimistic locking (without conflict resolver)
	// IMPORTANT: ETags must be quoted per RFC 7232
	cal.Props.SetText(ical.PropSummary, "Updated Event")

	// Quote ETag for If-Match header (remove quotes if present, then re-add)
	etag := strings.Trim(obj.ETag, "\"")
	quotedETag := "\"" + etag + "\""

	putOpts := &PutOptions{
		IfMatch: webdav.ConditionalMatch(quotedETag),
	}
	updated, err := client.PutCalendarObject(ctx, "/calendar/example.ics", cal, putOpts)
	if err != nil {
		if errors.Is(err, ErrPreconditionFailed) {
			fmt.Println("Event was modified by another client")
		} else {
			panic(err)
		}
	} else {
		fmt.Printf("Updated event with new ETag: %s\n", updated.ETag)
	}

	// Update event with automatic conflict resolution
	client.SetConflictResolver(&LastModifiedWinsResolver{})
	cal.Props.SetText(ical.PropSummary, "Auto-resolved Event")
	updated, err = client.PutCalendarObject(ctx, "/calendar/example.ics", cal, putOpts)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Updated event with automatic conflict resolution: %s\n", updated.ETag)

	// Query events with time range
	start := time.Now().AddDate(0, 0, -7) // Last week
	end := time.Now().AddDate(0, 0, 7)    // Next week

	query := &CalendarQuery{
		CompRequest: CalendarCompRequest{
			Props: []string{"getetag"},
			Comps: []CalendarCompRequest{{
				Name: "VCALENDAR",
				Comps: []CalendarCompRequest{{
					Name:  "VEVENT",
					Props: []string{"summary", "dtstart", "dtend"},
				}},
			}},
		},
		CompFilter: CompFilter{
			Name: "VCALENDAR",
			Comps: []CompFilter{{
				Name: "VEVENT",
			}},
		},
	}

	queryOpts := &QueryOptions{
		TimeRangeStart: &start,
		TimeRangeEnd:   &end,
	}
	events, err := client.QueryCalendar(ctx, "/calendar/", query, queryOpts)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Found %d events in time range\n", len(events))

	// Perform incremental sync
	result, err := client.SyncCalendar(ctx, "/calendar/", "", nil)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Initial sync: %d created, %d updated, %d deleted events\n",
		len(result.Created), len(result.Updated), len(result.Deleted))

	// Use sync token for next incremental sync
	result, err = client.SyncCalendar(ctx, "/calendar/", result.SyncToken, nil)
	if err != nil {
		if errors.Is(err, ErrSyncTokenExpired) {
			fmt.Println("Sync token expired, performing full sync")
			result, err = client.SyncCalendar(ctx, "/calendar/", "", nil)
		} else {
			panic(err)
		}
	}

	// Delete event
	err = client.DeleteCalendarObject(ctx, "/calendar/example.ics")
	if err != nil {
		panic(err)
	}
	fmt.Println("Event deleted successfully")
}

// Example_conflictResolution demonstrates conflict resolution policies.
func Example_conflictResolution() {
	// Default policy - last modified wins
	resolver := &LastModifiedWinsResolver{}

	local := &CalendarObject{
		Path:    "/calendar/event.ics",
		ETag:    "\"local-etag\"",
		ModTime: time.Now(),
	}

	remote := &CalendarObject{
		Path:    "/calendar/event.ics",
		ETag:    "\"remote-etag\"",
		ModTime: time.Now().Add(time.Hour), // More recent
	}

	decision := resolver.Resolve(local, remote)
	fmt.Printf("Conflict resolution decision: %s\n", decision) // Should be UseRemote

	// Custom policy - always use local
	customResolver := &AlwaysUseLocalResolver{}
	decision = customResolver.Resolve(local, remote)
	fmt.Printf("Custom resolver decision: %s\n", decision) // Should be UseLocal
}
