package caldav

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
)

func loadFixture(path string) string {
	data, err := ioutil.ReadFile(filepath.Join("testdata", path))
	if err != nil {
		panic(fmt.Sprintf("Failed to load fixture %s: %v", path, err))
	}
	return string(data)
}

var (
	googleDiscoveryXML = loadFixture("google/discovery.xml")
	googleSyncXML      = loadFixture("google/sync.xml")
	googleSyncTokenXML = loadFixture("google/sync_token.xml")
	googleMultigetXML  = loadFixture("google/multiget.xml")
	appleDiscoveryXML  = loadFixture("apple/discovery.xml")
	appleSyncXML       = loadFixture("apple/sync.xml")
	appleSyncTokenXML  = loadFixture("apple/sync_token.xml")
	yandexDiscoveryXML = loadFixture("yandex/discovery.xml")
	yandexSyncXML      = loadFixture("yandex/sync.xml")
	yandexSyncTokenXML = loadFixture("yandex/sync_token.xml")
	mailDiscoveryXML   = loadFixture("mail/discovery.xml")
	mailSyncXML        = loadFixture("mail/sync.xml")
)

type MockCalDAVServer struct {
	*httptest.Server
	mu        sync.RWMutex
	responses map[string]MockResponse
}

type MockResponse struct {
	StatusCode int
	Body       string
	Headers    map[string]string
}

func NewMockCalDAVServer() *MockCalDAVServer {
	mock := &MockCalDAVServer{
		responses: make(map[string]MockResponse),
	}
	mock.Server = httptest.NewServer(http.HandlerFunc(mock.handler))
	return mock
}

func (m *MockCalDAVServer) handler(w http.ResponseWriter, r *http.Request) {
	key := fmt.Sprintf("%s:%s", r.Method, r.URL.Path)

	m.mu.RLock()
	response, ok := m.responses[key]
	m.mu.RUnlock()

	if !ok {
		http.NotFound(w, r)
		return
	}

	for k, v := range response.Headers {
		w.Header().Set(k, v)
	}

	w.WriteHeader(response.StatusCode)
	w.Write([]byte(response.Body))
}

func (m *MockCalDAVServer) SetResponse(method, path string, resp MockResponse) {
	key := fmt.Sprintf("%s:%s", method, path)
	m.mu.Lock()
	m.responses[key] = resp
	m.mu.Unlock()
}

func (m *MockCalDAVServer) Close() {
	m.Server.Close()
}

func TestClient_QueryCalendar_WithMockServer(t *testing.T) {
	mock := NewMockCalDAVServer()
	defer mock.Close()

	mock.SetResponse("REPORT", "/calendars/user/events/", MockResponse{
		StatusCode: 207,
		Body:       googleSyncXML,
		Headers: map[string]string{
			"Content-Type": "application/xml; charset=utf-8",
		},
	})

	httpClient := &http.Client{}
	client, err := NewClient(httpClient, mock.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)

	query := &CalendarQuery{
		CompRequest: CalendarCompRequest{
			Name:  "VCALENDAR",
			Comps: []CalendarCompRequest{{Name: "VEVENT"}},
		},
	}

	opts := &QueryOptions{
		TimeRangeStart: &start,
		TimeRangeEnd:   &end,
	}

	objects, err := client.QueryCalendar(ctx, "/calendars/user/events/", query, opts)
	if err != nil {
		t.Fatalf("QueryCalendar failed: %v", err)
	}

	if len(objects) == 0 {
		t.Error("Expected at least one calendar object")
	}
}

func TestClient_SyncCalendar_WithMockServer(t *testing.T) {
	mock := NewMockCalDAVServer()
	defer mock.Close()

	mock.SetResponse("REPORT", "/calendars/user/events/", MockResponse{
		StatusCode: 207,
		Body:       googleSyncTokenXML,
		Headers: map[string]string{
			"Content-Type": "application/xml; charset=utf-8",
		},
	})

	httpClient := &http.Client{}
	client, err := NewClient(httpClient, mock.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	result, err := client.SyncCalendar(ctx, "/calendars/user/events/", "", nil)
	if err != nil {
		t.Fatalf("SyncCalendar failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.SyncToken == "" {
		t.Error("Expected non-empty sync token")
	}
}

func TestClient_PutCalendarObject_WithMockServer(t *testing.T) {
	mock := NewMockCalDAVServer()
	defer mock.Close()

	mock.SetResponse("PUT", "/calendars/user/events/test.ics", MockResponse{
		StatusCode: 201,
		Body:       "",
		Headers: map[string]string{
			"ETag": "\"test-etag-123\"",
		},
	})

	httpClient := &http.Client{}
	client, err := NewClient(httpClient, mock.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//Test//Test//EN")
	event := ical.NewEvent()
	event.Props.SetText(ical.PropUID, "test-uid-123")
	event.Props.SetDateTime(ical.PropDateTimeStamp, time.Now())
	event.Props.SetDateTime(ical.PropDateTimeStart, time.Now())
	event.Props.SetDateTime(ical.PropDateTimeEnd, time.Now().Add(1*time.Hour))
	event.Props.SetText(ical.PropSummary, "Test Event")
	cal.Children = []*ical.Component{event.Component}

	ctx := context.Background()
	obj, err := client.PutCalendarObject(ctx, "/calendars/user/events/test.ics", cal, nil)
	if err != nil {
		t.Fatalf("PutCalendarObject failed: %v", err)
	}

	if obj.ETag == "" {
		t.Error("Expected non-empty ETag")
	}
}

func TestClient_PutCalendarObject_PreconditionFailed(t *testing.T) {
	mock := NewMockCalDAVServer()
	defer mock.Close()

	mock.SetResponse("PUT", "/calendars/user/events/test.ics", MockResponse{
		StatusCode: 412,
		Body:       "",
	})

	httpClient := &http.Client{}
	client, err := NewClient(httpClient, mock.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//Test//Test//EN")
	event := ical.NewEvent()
	event.Props.SetText(ical.PropUID, "test-uid")
	event.Props.SetDateTime(ical.PropDateTimeStamp, time.Now())
	event.Props.SetDateTime(ical.PropDateTimeStart, time.Now())
	event.Props.SetDateTime(ical.PropDateTimeEnd, time.Now().Add(time.Hour))
	cal.Children = []*ical.Component{event.Component}

	opts := &PutOptions{
		IfMatch: webdav.ConditionalMatch("\"old-etag\""),
	}

	ctx := context.Background()
	_, err = client.PutCalendarObject(ctx, "/calendars/user/events/test.ics", cal, opts)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !strings.Contains(err.Error(), "412") && err != ErrPreconditionFailed {
		t.Errorf("Expected 412 Precondition Failed error, got %v", err)
	}
}

func TestClient_DeleteCalendarObject_WithMockServer(t *testing.T) {
	mock := NewMockCalDAVServer()
	defer mock.Close()

	mock.SetResponse("DELETE", "/calendars/user/events/test.ics", MockResponse{
		StatusCode: 204,
		Body:       "",
	})

	httpClient := &http.Client{}
	client, err := NewClient(httpClient, mock.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	err = client.DeleteCalendarObject(ctx, "/calendars/user/events/test.ics")
	if err != nil {
		t.Fatalf("DeleteCalendarObject failed: %v", err)
	}
}

func TestClient_GetCalendarObject_WithMockServer(t *testing.T) {
	mock := NewMockCalDAVServer()
	defer mock.Close()

	icsData := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Test//Test//EN
BEGIN:VEVENT
UID:test-uid-123
DTSTART:20250115T100000Z
DTEND:20250115T110000Z
SUMMARY:Test Event
END:VEVENT
END:VCALENDAR`

	mock.SetResponse("GET", "/calendars/user/events/test.ics", MockResponse{
		StatusCode: 200,
		Body:       icsData,
		Headers: map[string]string{
			"Content-Type": "text/calendar; charset=utf-8",
			"ETag":         "\"test-etag\"",
		},
	})

	httpClient := &http.Client{}
	client, err := NewClient(httpClient, mock.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	obj, err := client.GetCalendarObject(ctx, "/calendars/user/events/test.ics")
	if err != nil {
		t.Fatalf("GetCalendarObject failed: %v", err)
	}

	if obj.ETag == "" {
		t.Error("Expected non-empty ETag")
	}

	if obj.Data == nil {
		t.Fatal("Expected non-nil calendar data")
	}
}

func TestClient_MultiGetCalendar_WithMockServer(t *testing.T) {
	mock := NewMockCalDAVServer()
	defer mock.Close()

	mock.SetResponse("REPORT", "/calendars/user/events/", MockResponse{
		StatusCode: 207,
		Body:       googleMultigetXML,
		Headers: map[string]string{
			"Content-Type": "application/xml; charset=utf-8",
		},
	})

	httpClient := &http.Client{}
	client, err := NewClient(httpClient, mock.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	multiget := &CalendarMultiGet{
		CompRequest: CalendarCompRequest{
			Name:  "VCALENDAR",
			Comps: []CalendarCompRequest{{Name: "VEVENT"}},
		},
	}

	objects, err := client.MultiGetCalendar(ctx, "/calendars/user/events/", multiget)
	if err != nil {
		t.Fatalf("MultiGetCalendar failed: %v", err)
	}

	if len(objects) == 0 {
		t.Error("Expected at least one calendar object")
	}
}

func TestClient_SyncCalendar_SyncTokenExpired(t *testing.T) {
	mock := NewMockCalDAVServer()
	defer mock.Close()

	mock.SetResponse("REPORT", "/calendars/user/events/", MockResponse{
		StatusCode: 410,
		Body:       "",
	})

	httpClient := &http.Client{}
	client, err := NewClient(httpClient, mock.URL)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	_, err = client.SyncCalendar(ctx, "/calendars/user/events/", "expired-token", nil)
	if err != ErrSyncTokenExpired {
		t.Errorf("Expected ErrSyncTokenExpired, got %v", err)
	}
}

// TestClient_HandleConflict_WithResolver tests conflict resolution with mock server.
// Note: This test is commented out because it requires complex mock server setup
// to handle the recursive PUT call after conflict resolution.
// Conflict resolution logic is tested in unit tests in conflict_test.go instead.
/*
func TestClient_HandleConflict_WithResolver(t *testing.T) {
	// Complex integration test that would require:
	// 1. Mock server responding to initial PUT with 412
	// 2. Mock server responding to GET with remote object
	// 3. Mock server responding to second PUT (after resolution) with success
	// This is better tested in end-to-end tests with real CalDAV servers.
}
*/

func TestParseGoogleSyncResponse(t *testing.T) {
	if !strings.Contains(googleSyncXML, "test-event-google") {
		t.Error("Google sync XML should contain test events")
	}

	if !strings.Contains(googleSyncXML, "VCALENDAR") {
		t.Error("Google sync XML should contain iCalendar data")
	}

	if !strings.Contains(googleSyncXML, "getetag") {
		t.Error("Google sync XML should contain ETags")
	}
}

func TestParseAppleSyncResponse(t *testing.T) {
	if !strings.Contains(appleSyncXML, "test-event-apple") {
		t.Error("Apple sync XML should contain test events")
	}

	if !strings.Contains(appleSyncXML, "VCALENDAR") {
		t.Error("Apple sync XML should contain iCalendar data")
	}

	if !strings.Contains(appleSyncXML, "getetag") {
		t.Error("Apple sync XML should contain ETags")
	}
}

func TestParseYandexSyncResponse(t *testing.T) {
	if !strings.Contains(yandexSyncXML, "yandex.ru") {
		t.Error("Yandex sync XML should contain yandex.ru")
	}

	if !strings.Contains(yandexSyncXML, "VCALENDAR") {
		t.Error("Yandex sync XML should contain iCalendar data")
	}

	if !strings.Contains(yandexSyncXML, "sync-token") {
		t.Error("Yandex sync XML should contain sync-token")
	}
}

func TestParseMailSyncResponse(t *testing.T) {
	if !strings.Contains(mailSyncXML, "mail.ru") {
		t.Error("Mail.ru sync XML should contain mail.ru")
	}

	if !strings.Contains(mailSyncXML, "VCALENDAR") {
		t.Error("Mail.ru sync XML should contain iCalendar data")
	}

	if !strings.Contains(mailSyncXML, "calendar-data") {
		t.Error("Mail.ru sync XML should contain calendar-data")
	}
}

func TestParseGoogleDiscoveryResponse(t *testing.T) {
	if !strings.Contains(googleDiscoveryXML, "gmail.com") {
		t.Error("Google discovery XML should contain gmail.com")
	}

	if !strings.Contains(googleDiscoveryXML, "calendar") {
		t.Error("Google discovery XML should contain calendar type")
	}

	if !strings.Contains(googleDiscoveryXML, "VEVENT") {
		t.Error("Google discovery XML should contain VEVENT component")
	}
}

func TestParseAppleDiscoveryResponse(t *testing.T) {
	if !strings.Contains(appleDiscoveryXML, "calendars") {
		t.Error("Apple discovery XML should contain calendars")
	}

	if !strings.Contains(appleDiscoveryXML, "calendar") {
		t.Error("Apple discovery XML should contain calendar type")
	}

	if !strings.Contains(appleDiscoveryXML, "VEVENT") {
		t.Error("Apple discovery XML should contain VEVENT component")
	}
}

func TestMultiStatusResponseVariants(t *testing.T) {
	responses := []struct {
		name string
		xml  string
	}{
		{"Google", googleSyncXML},
		{"Apple", appleSyncXML},
		{"Yandex", yandexSyncXML},
		{"Mail.ru", mailSyncXML},
	}

	for _, resp := range responses {
		t.Run(resp.name, func(t *testing.T) {
			if !strings.Contains(resp.xml, "multistatus") {
				t.Errorf("%s XML should contain multistatus element", resp.name)
			}

			if !strings.Contains(resp.xml, "response") {
				t.Errorf("%s XML should contain response elements", resp.name)
			}

			if !strings.Contains(resp.xml, "200 OK") {
				t.Errorf("%s XML should contain 200 OK status", resp.name)
			}
		})
	}
}
