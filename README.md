# go-webdav

[![Go Reference](https://pkg.go.dev/badge/github.com/emersion/go-webdav.svg)](https://pkg.go.dev/github.com/emersion/go-webdav)

A Go library for [WebDAV], [CalDAV] and [CardDAV].

## Installation

```bash
go get github.com/emersion/go-webdav
```

## Components

### WebDAV

Basic WebDAV client and server implementation supporting:
- File operations (GET, PUT, DELETE, MKCOL)
- Properties (PROPFIND, PROPPATCH)
- Locking (LOCK, UNLOCK)
- Conditional requests (If-Match, If-None-Match)

### CardDAV

CardDAV client and server for managing contacts:
- Address book discovery
- Contact CRUD operations
- Contact queries and filtering

### CalDAV

CalDAV client and server for calendar operations with enhanced functionality:

#### Features (Original)
- Calendar discovery (principal, calendar-home-set)
- Calendar CRUD operations
- Calendar queries with filters (comp-filter, prop-filter)
- Recurring event expansion

#### Features (New)
- **Conditional Operations**: If-Match/If-None-Match headers (RFC 7232) for optimistic locking
- **Incremental Synchronization**: RFC 6578 sync-collection REPORT for efficient syncing
- **Time Range Filtering**: Query events within specific date ranges
- **Conflict Resolution**: Automatic conflict resolution with pluggable policies
- **Provider Compatibility**: Tested with Google Calendar, Apple iCloud, Yandex, Mail.ru

## CalDAV Usage

### Basic Operations

```go
import (
    "github.com/emersion/go-webdav/caldav"
    "github.com/emersion/go-webdav"
    "github.com/emersion/go-ical"
)

// Create client
httpClient := webdav.HTTPClientWithBasicAuth(nil, username, password)
client, err := caldav.NewClient(httpClient, "https://caldav.example.com")

// Get event
obj, err := client.GetCalendarObject(ctx, "/calendar/event.ics")

// Create event (fails if exists)
cal := ical.NewCalendar()
// ... configure calendar ...
opts := &caldav.PutOptions{
    IfNoneMatch: webdav.ConditionalMatch("*"),
}
obj, err := client.PutCalendarObject(ctx, "/calendar/event.ics", cal, opts)

// Update with optimistic locking
opts = &caldav.PutOptions{
    IfMatch: webdav.ConditionalMatch("\"" + strings.Trim(obj.ETag, "\"") + "\""),
}
obj, err = client.PutCalendarObject(ctx, "/calendar/event.ics", cal, opts)
if errors.Is(err, caldav.ErrPreconditionFailed) {
    // Conflict detected
}

// Delete (idempotent)
err = client.DeleteCalendarObject(ctx, "/calendar/event.ics")
```

### Incremental Synchronization

```go
// Initial sync - get all events + sync token
result, err := client.SyncCalendar(ctx, "/calendar/", "", nil)
fmt.Printf("Initial: %d events, token: %s\n", len(result.Created), result.SyncToken)

// Store result.SyncToken for next sync

// Later: incremental sync - get only changes
result, err = client.SyncCalendar(ctx, "/calendar/", storedToken, nil)
if errors.Is(err, caldav.ErrSyncTokenExpired) {
    // Token expired - fall back to full sync
    result, err = client.SyncCalendar(ctx, "/calendar/", "", nil)
}

fmt.Printf("Changes: %d created, %d updated, %d deleted\n",
    len(result.Created), len(result.Updated), len(result.Deleted))
```

### Time Range Queries

```go
start := time.Now()
end := start.AddDate(0, 1, 0)

opts := &caldav.QueryOptions{
    TimeRangeStart: &start,
    TimeRangeEnd:   &end,
}

query := &caldav.CalendarQuery{
    CompRequest: caldav.CalendarCompRequest{
        Name:  "VCALENDAR",
        Comps: []caldav.CalendarCompRequest{{Name: "VEVENT"}},
    },
    CompFilter: caldav.CompFilter{
        Name:  "VCALENDAR",
        Comps: []caldav.CompFilter{{Name: "VEVENT"}},
    },
}

events, err := client.QueryCalendar(ctx, "/calendar/", query, opts)
```

### Conflict Resolution

```go
// Set automatic conflict resolution strategy
client.SetConflictResolver(&caldav.LastModifiedWinsResolver{})

// On HTTP 412, resolver automatically handles conflict
opts := &caldav.PutOptions{
    IfMatch: webdav.ConditionalMatch("\"" + etag + "\""),
}
obj, err := client.PutCalendarObject(ctx, path, cal, opts)

// Built-in resolvers:
// - LastModifiedWinsResolver: Use version with most recent modification time
// - AlwaysUseLocalResolver: Always use local changes (force overwrite)
// - AlwaysUseRemoteResolver: Always use server version (discard local)
// - nil (default): Return ErrPreconditionFailed for manual handling
```

### Client Responsibilities

#### ETag Formatting

**Critical**: ETags in conditional headers (If-Match, If-None-Match) **MUST be quoted** per RFC 7232.

Different providers return ETags in different formats:
- **Google/Apple**: `"abc123"` (quoted, RFC-compliant)
- **Yandex/Mail.ru**: `abc123` (unquoted, non-compliant)

**Your responsibility**: Always normalize and quote ETags:

```go
// ✅ Correct pattern
etag := strings.Trim(obj.ETag, "\"")  // Normalize (remove quotes if present)
quotedETag := "\"" + etag + "\""      // Quote for header
opts := &caldav.PutOptions{
    IfMatch: webdav.ConditionalMatch(quotedETag),
}

// ❌ Wrong - unquoted ETag will fail
opts := &caldav.PutOptions{
    IfMatch: webdav.ConditionalMatch("abc123"),
}
```

**Recommended helper functions**:

```go
func NormalizeETag(etag string) string {
    return strings.Trim(etag, "\"")
}

func QuoteETag(etag string) string {
    if etag == "*" {
        return "*"  // Wildcard not quoted
    }
    return "\"" + strings.Trim(etag, "\"") + "\""
}
```

#### Error Handling

Handle provider-specific quirks:

```go
// Standard conflict (Google, Apple, Mail.ru)
if errors.Is(err, caldav.ErrPreconditionFailed) {
    // Handle conflict
}

// Yandex-specific: returns 504 instead of 412 for conflicts
if strings.Contains(err.Error(), "504") || strings.Contains(err.Error(), "502") {
    // Treat as conflict
}

// Sync token expired
if errors.Is(err, caldav.ErrSyncTokenExpired) {
    // Fall back to full sync
}
```

### Provider Compatibility

| Provider        | CRUD | Query | Sync | Time-Range | If-Match | Notes                               |
|-----------------|------|-------|------|------------|----------|-------------------------------------|
| Google Calendar | ✅    | ✅     | ✅    | ✅          | ✅        | 100% compatible                     |
| Apple iCloud    | ✅    | ✅     | ✅    | ✅          | ✅        | 100% compatible                     |
| Yandex          | ✅    | ✅     | ✅    | ✅          | ✅        | Returns 504 for conflicts (not 412) |
| Mail.ru         | ✅    | ✅     | ❌    | ✅          | ✅        | Sync not supported (use Query)      |

**Details**: Authentication methods, ETag handling patterns, provider quirks, and troubleshooting → [cmd/caldav-test/README.md](cmd/caldav-test/README.md)

## Testing

### Unit Tests

Test library code with mock data:

```bash
task test
# or
go test -v -short ./...
```

### Integration Tests

Test with mock CalDAV server:

```bash
task integration
# or
go test -v ./caldav/
```

### Manual Testing

Test against real CalDAV providers (Google, Apple, Yandex, Mail.ru):

```bash
# Setup
cp .env.example .env  # Configure credentials

# Test all providers
task manual-test

# Test specific provider
task manual-test-google
task manual-test-apple
task manual-test-yandex
task manual-test-mail

# Or run directly
cd cmd/caldav-test
go run main.go google
```

**Manual testing utility documentation**: [cmd/caldav-test/README.md](cmd/caldav-test/README.md)

## Available Commands (Taskfile)

```bash
# Development
task lint          # Run static analysis (go vet, go mod tidy)
task build         # Build all packages
task all           # Run lint, build, and all tests

# Testing
task test          # Run unit tests
task integration   # Run integration tests with mock server
task coverage      # Run tests with coverage report

# Manual testing with real providers
task manual-test             # Test all providers
task manual-test-google      # Test Google Calendar only
task manual-test-apple       # Test Apple iCloud only
task manual-test-yandex      # Test Yandex Calendar only
task manual-test-mail        # Test Mail.ru Calendar only
```

## Documentation

- [GoDoc](https://pkg.go.dev/github.com/emersion/go-webdav) - API reference
- [Manual Testing Utility](cmd/caldav-test/README.md) - Provider details, quirks, and troubleshooting

## License

MIT

[WebDAV]: https://tools.ietf.org/html/rfc4918
[CalDAV]: https://tools.ietf.org/html/rfc4791
[CardDAV]: https://tools.ietf.org/html/rfc6352
