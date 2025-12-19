# CalDAV Manual Test Utility

Command-line tool for validating CalDAV client implementation against real providers before production deployment.

## Why Use This Tool

- **Validate provider compatibility** - Test your CalDAV integration with Google, Apple, Yandex, Mail.ru
- **Discover provider quirks** - Identify non-standard behavior (e.g., Yandex 504 conflicts, Mail.ru no sync)
- **Verify authentication** - Test OAuth tokens, app-specific passwords, basic auth
- **Debug issues** - Understand what works and what doesn't with each provider

## Quick Start

```bash
# 1. Configure credentials
cd /path/to/go-webdav
cp .env.example .env
# Edit .env with your credentials

# 2. Run tests
task manual-test-google     # Test one provider
task manual-test            # Test all providers
```

## Setup

### 1. Get Credentials

**Google Calendar** (OAuth 2.0):
- Go to [Google Cloud Console](https://console.cloud.google.com)
- Create OAuth 2.0 credentials
- Get access token with scope: `https://www.googleapis.com/auth/calendar`

**Apple iCloud** (App-specific password):
- Go to [appleid.apple.com](https://appleid.apple.com)
- Sign in → Security → App-Specific Passwords → Generate
- Use format: `abcd-efgh-ijkl-mnop`

**Yandex** (Basic auth):
- Use your Yandex account password or generate app password

**Mail.ru** (Basic auth):
- Use your Mail.ru account password

### 2. Configure `.env`

Edit `.env` in repository root:

```bash
# Google
GOOGLE_ACCESS_TOKEN=ya29.a0...
GOOGLE_EMAIL=user@gmail.com

# Apple
APPLE_USERNAME=user@icloud.com
APPLE_PASSWORD=abcd-efgh-ijkl-mnop

# Yandex
YANDEX_USERNAME=user@yandex.ru
YANDEX_PASSWORD=your_password

# Mail.ru
MAIL_USERNAME=user@mail.ru
MAIL_PASSWORD=your_password
```

### 3. Run Tests

```bash
# From repository root
task manual-test              # All providers
task manual-test-google       # Specific provider

# Or directly
cd cmd/caldav-test
go run main.go                # All providers
go run main.go google         # Specific provider
```

## Understanding Test Results

### Output Format

```
========== Testing Google ==========
Base URL: https://apidata.googleusercontent.com
   Discovering calendar...
   Calendar URL: https://apidata.googleusercontent.com/caldav/v2/user@gmail.com/events/

✅ [Google] Create (0.45s): ETag: "abc123"
✅ [Google] Read (0.23s): Parsed successfully
✅ [Google] Update (0.38s)
✅ [Google] Conflicts (0.29s): 412 detected correctly
✅ [Google] Query (0.34s): Found 15 events
✅ [Google] Sync (0.31s): 0 created, 0 updated, 0 deleted
✅ [Google] Time-Range (0.28s)
✅ [Google] Delete (0.42s)

========== RESULTS ==========
✅ Google: 8/8 (100%)
```

### What Each Test Validates

| Test        | Validates                                                   | Critical for           |
|-------------|-------------------------------------------------------------|------------------------|
| Create      | PUT with If-None-Match (prevents duplicates)                | Creating events        |
| Read        | GET and iCalendar parsing                                   | Fetching events        |
| Update      | PUT with If-Match (optimistic locking)                      | Updating events        |
| Conflicts   | Wrong ETag detection (expects HTTP 412)                     | Conflict handling      |
| Query       | calendar-query REPORT                                       | Fetching multiple events |
| Sync        | sync-collection REPORT (RFC 6578)                           | Incremental sync       |
| Time-Range  | Query with date filter                                      | Date-range queries     |
| Delete      | DELETE (idempotent)                                         | Removing events        |

### Expected Results by Provider

| Provider | Score      | Known Issues                                  |
|----------|------------|-----------------------------------------------|
| Google   | 8/8 (100%) | None - all tests pass                         |
| Apple    | 8/8 (100%) | None - all tests pass                         |
| Yandex   | 7/8 (88%)  | ❌ Conflicts test fails (returns 504, not 412) |
| Mail.ru  | 7/8 (88%)  | ❌ Sync test fails (not supported)             |

**Important**: Failed tests for Yandex/Mail.ru are **expected** and **not bugs** - they indicate known provider limitations.

## Interpreting Failures

### ✅ Success (All Green)

```
✅ [Google] Create (0.45s): ETag: "abc123"
```

**Meaning**: Operation worked correctly. ETag returned for optimistic locking.

### ❌ Unexpected Failure (Red)

```
❌ [Google] Create (0.45s)
   Error: 401 Unauthorized
```

**Action**: Check credentials. Token may be expired or invalid.

### ⚠️ Expected Failure (Yellow - Not a Bug)

```
❌ [Yandex] Conflicts (0.14s): 504 Gateway Timeout
```

**Meaning**: Known Yandex quirk - returns 504 instead of 412 for conflicts. Your code should handle both:

```go
if errors.Is(err, caldav.ErrPreconditionFailed) || 
   strings.Contains(err.Error(), "504") {
    // Handle conflict
}
```

```
❌ [Mail.ru] Sync (0.05s): 404 Not Found
```

**Meaning**: Mail.ru doesn't support RFC 6578 sync. Use Query for full synchronization:

```go
// ❌ Don't use Sync with Mail.ru
result, err := client.SyncCalendar(ctx, path, token, nil)

// ✅ Use Query instead
events, err := client.QueryCalendar(ctx, path, query, nil)
```

## Provider-Specific Details

### Google Calendar ✅ 100%

- **Authentication**: OAuth 2.0 (token expires - regenerate as needed)
- **ETag Format**: Quoted (`"abc123"`)
- **Quirks**: None
- **Production Ready**: Yes

### Apple iCloud ✅ 100%

- **Authentication**: App-specific password ([generate here](https://appleid.apple.com))
- **ETag Format**: Quoted (`"mjbdov4h"`)
- **Quirks**: None
- **Production Ready**: Yes

### Yandex Calendar ⚠️ 100% (with quirks)

- **Authentication**: Basic auth or App token
- **ETag Format**: Unquoted (`1764836851523`) - RFC violation
- **Quirks**: Returns 504 instead of 412 for conflicts
- **Production Ready**: Yes (handle 504 as conflict)

**Code pattern**:
```go
if err != nil {
    if strings.Contains(err.Error(), "412") || 
       err == caldav.ErrPreconditionFailed {
        // Standard conflict
    } else if strings.Contains(err.Error(), "504") || 
              strings.Contains(err.Error(), "502") {
        // Yandex-specific conflict
    }
}
```

### Mail.ru Calendar ⚠️ 88%

- **Authentication**: Basic auth
- **ETag Format**: Unquoted (`b2e335cca8c50eeeca4ef01476f17cfb6f496e92`) - RFC violation
- **Quirks**: No sync-collection support
- **Production Ready**: Yes (use Query instead of Sync)

## ETag Handling Best Practices

**Problem**: Different providers return ETags differently (quoted vs unquoted).

**Solution**: Always normalize and quote:

```go
// Reading from provider
obj, err := client.GetCalendarObject(ctx, path)
normalizedETag := strings.Trim(obj.ETag, "\"")  // Remove quotes
// Store normalizedETag in your database

// Writing to provider
storedETag := db.GetETag(eventID)
quotedETag := "\"" + storedETag + "\""  // Add quotes for RFC 7232
opts := &caldav.PutOptions{
    IfMatch: webdav.ConditionalMatch(quotedETag),
}
```

**Helper functions** (copy to your code):

```go
func NormalizeETag(etag string) string {
    return strings.Trim(etag, "\"")
}

func QuoteETag(etag string) string {
    if etag == "*" {
        return "*"
    }
    return "\"" + strings.Trim(etag, "\"") + "\""
}
```

## Troubleshooting

### Authentication Errors

**401 Unauthorized**
- Google: Token expired → regenerate OAuth token
- Apple: Invalid password → regenerate at appleid.apple.com
- Yandex/Mail.ru: Check username/password

### Discovery Failures

**No calendars found**
- Verify credentials have calendar access
- Check base URL is correct

**Multiple calendars found**
- Utility uses first calendar by default
- Expected behavior

### Network Issues

**Timeout (context deadline exceeded)**
- Network connectivity issue
- Provider may be slow/down
- Try again later

**SSL/TLS errors**
- Should not occur (tool skips cert verification)
- Check Go installation if it does

## CI/CD Integration

Run without `.env` file by exporting environment variables:

```bash
# GitHub Actions / Jenkins / etc.
export GOOGLE_ACCESS_TOKEN="ya29.a0..."
export GOOGLE_EMAIL="user@gmail.com"
go run cmd/caldav-test/main.go google

# Exit codes
# 0 = All tests passed
# 1 = Some tests failed
# 2 = Fatal error (can't connect, etc.)
```

## Before Production Deployment

Run these tests to ensure your CalDAV integration works:

1. ✅ **Test with your target providers** - Run against the providers you'll use
2. ✅ **Verify authentication** - Ensure tokens/passwords work
3. ✅ **Check expected failures** - Confirm Yandex 504 and Mail.ru 404 are handled
4. ✅ **Test ETag handling** - Verify your code normalizes and quotes ETags
5. ✅ **Review conflict resolution** - Ensure your conflict strategy works

## See Also

- [Main README](../../README.md) - Library usage and API reference
- [Taskfile](../../Taskfile.yml) - Available commands
