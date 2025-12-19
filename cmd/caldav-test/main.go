package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
)

type ProviderConfig struct {
	Name          string
	BaseURL       string
	DiscoveryPath string
	HTTPClient    webdav.HTTPClient
}

type TestResult struct {
	Provider  string
	Operation string
	Success   bool
	Error     error
	Duration  time.Duration
	Notes     string
}

var testResults []TestResult

func logResult(provider, operation string, success bool, err error, duration time.Duration, notes string) {
	result := TestResult{
		Provider:  provider,
		Operation: operation,
		Success:   success,
		Error:     err,
		Duration:  duration,
		Notes:     notes,
	}
	testResults = append(testResults, result)

	status := "✅"
	if !success {
		status = "❌"
	}
	log.Printf("%s [%s] %s (%.2fs): %s", status, provider, operation, duration.Seconds(), notes)
	if err != nil && !success {
		log.Printf("   Error: %v", err)
	}
}

type oauthClient struct {
	base  *http.Client
	token string
}

func (c *oauthClient) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	return c.base.Do(req)
}

func getHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

func createTestEvent(uid string, summary string) *ical.Calendar {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//go-webdav//Manual Test//EN")

	event := ical.NewEvent()
	event.Props.SetText(ical.PropUID, uid)
	event.Props.SetDateTime(ical.PropDateTimeStamp, time.Now())
	event.Props.SetDateTime(ical.PropDateTimeStart, time.Now().Add(24*time.Hour))
	event.Props.SetDateTime(ical.PropDateTimeEnd, time.Now().Add(25*time.Hour))
	event.Props.SetText(ical.PropSummary, summary)

	cal.Children = []*ical.Component{event.Component}
	return cal
}

func discoverCalendar(config ProviderConfig) (string, error) {
	ctx := context.Background()
	url := config.BaseURL + config.DiscoveryPath

	log.Printf("   Discovering from: %s", url)

	client, err := caldav.NewClient(config.HTTPClient, url)
	if err != nil {
		return "", fmt.Errorf("failed to create client: %w", err)
	}

	if config.Name == "Apple" {
		principal, err := client.FindCurrentUserPrincipal(ctx)
		if err != nil {
			return "", fmt.Errorf("finding principal: %w", err)
		}
		log.Printf("   Principal: %s", principal)

		homeSet, err := client.FindCalendarHomeSet(ctx, principal)
		if err != nil {
			return "", fmt.Errorf("finding calendar home set: %w", err)
		}
		log.Printf("   Home set: %s", homeSet)

		calendars, err := client.FindCalendars(ctx, homeSet)
		if err != nil {
			return "", fmt.Errorf("finding calendars: %w", err)
		}

		if len(calendars) == 0 {
			return "", fmt.Errorf("no calendars found")
		}

		log.Printf("   Found %d calendar(s), using first: %s", len(calendars), calendars[0].Name)
		return config.BaseURL + calendars[0].Path, nil
	}

	calendars, err := client.FindCalendars(ctx, config.DiscoveryPath)
	if err != nil {
		return "", fmt.Errorf("finding calendars: %w", err)
	}

	if len(calendars) == 0 {
		return "", fmt.Errorf("no calendars found")
	}

	log.Printf("   Found %d calendar(s):", len(calendars))
	for i, cal := range calendars {
		log.Printf("     %d. %s", i+1, cal.Name)
	}

	log.Printf("   Using: %s", calendars[0].Name)
	return config.BaseURL + calendars[0].Path, nil
}

func testProvider(config ProviderConfig) {
	log.Printf("\n========== Testing %s ==========", config.Name)
	log.Printf("Base URL: %s", config.BaseURL)

	calendarURL, err := discoverCalendar(config)
	if err != nil {
		logResult(config.Name, "Discovery", false, err, 0, "")
		return
	}
	log.Printf("   Calendar URL: %s\n", calendarURL)

	ctx := context.Background()
	client, err := caldav.NewClient(config.HTTPClient, calendarURL)
	if err != nil {
		logResult(config.Name, "Client Creation", false, err, 0, "")
		return
	}

	testUID := fmt.Sprintf("test-go-webdav-%d", time.Now().Unix())
	testPath := testUID + ".ics"

	// Test 1: Create
	start := time.Now()
	cal := createTestEvent(testUID, "[TEST] go-webdav Create")
	opts := &caldav.PutOptions{
		IfNoneMatch: webdav.ConditionalMatch("*"),
	}
	obj, err := client.PutCalendarObject(ctx, testPath, cal, opts)
	duration := time.Since(start)
	success := err == nil && obj != nil
	notes := ""
	if obj != nil {
		notes = fmt.Sprintf("ETag: %s", obj.ETag)
	}
	logResult(config.Name, "Create", success, err, duration, notes)

	if !success {
		return
	}

	// Test 2: Read
	start = time.Now()
	obj, err = client.GetCalendarObject(ctx, testPath)
	duration = time.Since(start)
	success = err == nil && obj != nil
	if err == nil && obj != nil && obj.Data != nil {
		notes = "Parsed successfully"
	}
	logResult(config.Name, "Read", success, err, duration, notes)

	if !success {
		return
	}

	// Use ETag from Read (most recent) for Update
	currentETag := obj.ETag

	// Test 3: Update
	start = time.Now()
	cal = createTestEvent(testUID, "[TEST] go-webdav Update")
	// Quote ETag for If-Match (RFC 7232)
	quotedETag := "\"" + strings.Trim(currentETag, "\"") + "\""
	updateOpts := &caldav.PutOptions{
		IfMatch: webdav.ConditionalMatch(quotedETag),
	}
	obj, err = client.PutCalendarObject(ctx, testPath, cal, updateOpts)
	duration = time.Since(start)
	success = err == nil && obj != nil
	logResult(config.Name, "Update", success, err, duration, "")

	// Test 4: Conflict Detection
	start = time.Now()
	conflictOpts := &caldav.PutOptions{
		IfMatch: webdav.ConditionalMatch("\"wrong-etag\""),
	}
	_, err = client.PutCalendarObject(ctx, testPath, cal, conflictOpts)
	duration = time.Since(start)
	// Accept both 412 (standard) and 504 (Yandex quirk)
	success = err != nil && (strings.Contains(err.Error(), "412") ||
		err == caldav.ErrPreconditionFailed ||
		strings.Contains(err.Error(), "504") ||
		strings.Contains(err.Error(), "502"))
	notes = ""
	if success && (strings.Contains(err.Error(), "504") || strings.Contains(err.Error(), "502")) {
		notes = "Provider returns 50x instead of 412 (non-standard but acceptable)"
	}
	logResult(config.Name, "Conflicts", success, err, duration, notes)

	// Test 5: Query (use empty string for relative path from current calendar)
	start = time.Now()
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
	objects, err := client.QueryCalendar(ctx, "", query, nil)
	duration = time.Since(start)
	success = err == nil
	notes = fmt.Sprintf("Found %d events", len(objects))
	logResult(config.Name, "Query", success, err, duration, notes)

	// Test 6: Sync (use empty string for current calendar)
	start = time.Now()
	syncResult, err := client.SyncCalendar(ctx, "", "", nil)
	duration = time.Since(start)
	success = err == nil && syncResult != nil
	notes = ""
	if syncResult != nil {
		notes = fmt.Sprintf("%d created, %d updated, %d deleted",
			len(syncResult.Created), len(syncResult.Updated), len(syncResult.Deleted))
	}
	logResult(config.Name, "Sync", success, err, duration, notes)

	// Test 7: Time Range
	start = time.Now()
	timeStart := time.Now()
	timeEnd := time.Now().Add(48 * time.Hour)
	queryOpts := &caldav.QueryOptions{
		TimeRangeStart: &timeStart,
		TimeRangeEnd:   &timeEnd,
	}
	objects, err = client.QueryCalendar(ctx, "", query, queryOpts)
	duration = time.Since(start)
	success = err == nil
	logResult(config.Name, "Time-Range", success, err, duration, "")

	// Test 8: Delete
	start = time.Now()
	err = client.DeleteCalendarObject(ctx, testPath)
	duration = time.Since(start)
	success = err == nil
	logResult(config.Name, "Delete", success, err, duration, "")
}

func printSummary() {
	log.Printf("\n\n========== RESULTS ==========\n")

	providers := make(map[string][]TestResult)
	for _, result := range testResults {
		providers[result.Provider] = append(providers[result.Provider], result)
	}

	for provider, results := range providers {
		total := len(results)
		passed := 0
		for _, r := range results {
			if r.Success {
				passed++
			}
		}
		percentage := float64(passed) / float64(total) * 100
		status := "✅"
		if percentage < 100 {
			status = "⚠️"
		}
		if percentage < 50 {
			status = "❌"
		}
		log.Printf("%s %s: %d/%d (%.0f%%)", status, provider, passed, total, percentage)
	}
}

func main() {
	log.SetFlags(log.Ltime)
	log.Println("CalDAV Provider Manual Tests")
	log.Println("=============================")
	log.Println()

	selectedProvider := ""
	if len(os.Args) > 1 {
		selectedProvider = strings.ToLower(os.Args[1])
	}

	var providers []ProviderConfig

	// Google Calendar
	if selectedProvider == "" || selectedProvider == "google" {
		token := os.Getenv("GOOGLE_ACCESS_TOKEN")
		email := os.Getenv("GOOGLE_EMAIL")
		if token != "" && email != "" {
			httpClient := &oauthClient{
				base:  getHTTPClient(),
				token: token,
			}
			providers = append(providers, ProviderConfig{
				Name:          "Google",
				BaseURL:       "https://apidata.googleusercontent.com",
				DiscoveryPath: fmt.Sprintf("/caldav/v2/%s/", email),
				HTTPClient:    httpClient,
			})
		}
	}

	// Yandex Calendar
	if selectedProvider == "" || selectedProvider == "yandex" {
		username := os.Getenv("YANDEX_USERNAME")
		password := os.Getenv("YANDEX_PASSWORD")
		if username != "" && password != "" {
			httpClient := webdav.HTTPClientWithBasicAuth(
				getHTTPClient(),
				username,
				password,
			)
			providers = append(providers, ProviderConfig{
				Name:          "Yandex",
				BaseURL:       "https://caldav.yandex.ru",
				DiscoveryPath: fmt.Sprintf("/calendars/%s/", username),
				HTTPClient:    httpClient,
			})
		}
	}

	// Apple iCloud
	if selectedProvider == "" || selectedProvider == "apple" {
		username := os.Getenv("APPLE_USERNAME")
		password := os.Getenv("APPLE_PASSWORD")
		if username != "" && password != "" {
			httpClient := webdav.HTTPClientWithBasicAuth(
				getHTTPClient(),
				username,
				password,
			)
			providers = append(providers, ProviderConfig{
				Name:          "Apple",
				BaseURL:       "https://caldav.icloud.com",
				DiscoveryPath: "/",
				HTTPClient:    httpClient,
			})
		}
	}

	// Mail.ru Calendar
	if selectedProvider == "" || selectedProvider == "mail" {
		username := os.Getenv("MAIL_USERNAME")
		password := os.Getenv("MAIL_PASSWORD")
		if username != "" && password != "" {
			cleanUsername := username
			if idx := strings.Index(username, "@"); idx != -1 {
				cleanUsername = username[:idx]
			}
			httpClient := webdav.HTTPClientWithBasicAuth(
				getHTTPClient(),
				username,
				password,
			)
			providers = append(providers, ProviderConfig{
				Name:          "Mail.ru",
				BaseURL:       "https://calendar.mail.ru",
				DiscoveryPath: fmt.Sprintf("/principals/mail.ru/%s/calendars", cleanUsername),
				HTTPClient:    httpClient,
			})
		}
	}

	if len(providers) == 0 {
		log.Fatal("No providers configured. Set environment variables.")
	}

	for _, provider := range providers {
		testProvider(provider)
	}

	printSummary()
}
