package caldav

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/internal"
)

// DiscoverContextURL performs a DNS-based CardDAV service discovery as
// described in RFC 6352 section 11. It returns the URL to the CardDAV server.
func DiscoverContextURL(ctx context.Context, domain string) (string, error) {
	return internal.DiscoverContextURL(ctx, "caldav", domain)
}

// Client provides access to a remote CardDAV server.
type Client struct {
	*webdav.Client

	ic               *internal.Client
	conflictResolver ConflictResolver
}

func NewClient(c webdav.HTTPClient, endpoint string) (*Client, error) {
	wc, err := webdav.NewClient(c, endpoint)
	if err != nil {
		return nil, err
	}
	ic, err := internal.NewClient(c, endpoint)
	if err != nil {
		return nil, err
	}
	return &Client{wc, ic, nil}, nil
}

// SetConflictResolver sets the conflict resolution strategy for this client.
// Pass nil to disable automatic conflict resolution (default behavior).
func (c *Client) SetConflictResolver(resolver ConflictResolver) {
	c.conflictResolver = resolver
}

func (c *Client) FindCalendarHomeSet(ctx context.Context, principal string) (string, error) {
	propfind := internal.NewPropNamePropFind(calendarHomeSetName)
	resp, err := c.ic.PropFindFlat(ctx, principal, propfind)
	if err != nil {
		return "", err
	}

	var prop calendarHomeSet
	if err := resp.DecodeProp(&prop); err != nil {
		return "", err
	}

	return prop.Href.Path, nil
}

func (c *Client) FindCalendars(ctx context.Context, calendarHomeSet string) ([]Calendar, error) {
	propfind := internal.NewPropNamePropFind(
		internal.ResourceTypeName,
		internal.DisplayNameName,
		calendarDescriptionName,
		maxResourceSizeName,
		supportedCalendarComponentSetName,
	)
	ms, err := c.ic.PropFind(ctx, calendarHomeSet, internal.DepthOne, propfind)
	if err != nil {
		return nil, err
	}

	l := make([]Calendar, 0, len(ms.Responses))
	errs := make([]error, 0, len(ms.Responses))
	for _, resp := range ms.Responses {
		path, err := resp.Path()
		if err != nil {
			errs = append(errs, err)
			continue
		}

		var resType internal.ResourceType
		if err := resp.DecodeProp(&resType); err != nil {
			return nil, err
		}
		if !resType.Is(calendarName) {
			continue
		}

		var desc calendarDescription
		if err := resp.DecodeProp(&desc); err != nil && !internal.IsNotFound(err) {
			return nil, err
		}

		var dispName internal.DisplayName
		if err := resp.DecodeProp(&dispName); err != nil && !internal.IsNotFound(err) {
			return nil, err
		}

		var maxResSize maxResourceSize
		if err := resp.DecodeProp(&maxResSize); err != nil && !internal.IsNotFound(err) {
			return nil, err
		}
		if maxResSize.Size < 0 {
			return nil, fmt.Errorf("carddav: max-resource-size must be a positive integer")
		}

		var supportedCompSet supportedCalendarComponentSet
		if err := resp.DecodeProp(&supportedCompSet); err != nil && !internal.IsNotFound(err) {
			return nil, err
		}

		compNames := make([]string, 0, len(supportedCompSet.Comp))
		for _, comp := range supportedCompSet.Comp {
			compNames = append(compNames, comp.Name)
		}

		l = append(l, Calendar{
			Path:                  path,
			Name:                  dispName.Name,
			Description:           desc.Description,
			MaxResourceSize:       maxResSize.Size,
			SupportedComponentSet: compNames,
		})
	}

	return l, errors.Join(errs...)
}

func encodeCalendarCompReq(c *CalendarCompRequest) (*comp, error) {
	encoded := comp{Name: c.Name}

	if c.AllProps {
		encoded.Allprop = &struct{}{}
	}
	for _, name := range c.Props {
		encoded.Prop = append(encoded.Prop, prop{Name: name})
	}

	if c.AllComps {
		encoded.Allcomp = &struct{}{}
	}
	for _, child := range c.Comps {
		encodedChild, err := encodeCalendarCompReq(&child)
		if err != nil {
			return nil, err
		}
		encoded.Comp = append(encoded.Comp, *encodedChild)
	}

	return &encoded, nil
}

func encodeCalendarReq(c *CalendarCompRequest) (*internal.Prop, error) {
	compReq, err := encodeCalendarCompReq(c)
	if err != nil {
		return nil, err
	}

	expandReq := encodeExpandRequest(c.Expand)

	calDataReq := calendarDataReq{Comp: compReq, Expand: expandReq}

	getLastModReq := internal.NewRawXMLElement(internal.GetLastModifiedName, nil, nil)
	getETagReq := internal.NewRawXMLElement(internal.GetETagName, nil, nil)
	return internal.EncodeProp(&calDataReq, getLastModReq, getETagReq)
}

func encodeCompFilter(filter *CompFilter) *compFilter {
	encoded := compFilter{Name: filter.Name}
	if !filter.Start.IsZero() || !filter.End.IsZero() {
		encoded.TimeRange = &timeRange{
			Start: dateWithUTCTime(filter.Start),
			End:   dateWithUTCTime(filter.End),
		}
	}
	for _, child := range filter.Comps {
		encoded.CompFilters = append(encoded.CompFilters, *encodeCompFilter(&child))
	}
	for _, pf := range filter.Props {
		encoded.PropFilters = append(encoded.PropFilters, *encodePropFilter(&pf))
	}
	return &encoded
}

func encodePropFilter(filter *PropFilter) *propFilter {
	encoded := propFilter{Name: filter.Name}
	if !filter.Start.IsZero() || !filter.End.IsZero() {
		encoded.TimeRange = &timeRange{
			Start: dateWithUTCTime(filter.Start),
			End:   dateWithUTCTime(filter.End),
		}
	}
	encoded.TextMatch = encodeTextMatch(filter.TextMatch)
	for _, pf := range filter.ParamFilter {
		encoded.ParamFilter = append(encoded.ParamFilter, encodeParamFilter(pf))
	}
	return &encoded
}

func encodeParamFilter(pf ParamFilter) paramFilter {
	encoded := paramFilter{
		Name:      pf.Name,
		TextMatch: encodeTextMatch(pf.TextMatch),
	}
	return encoded
}

func encodeTextMatch(tm *TextMatch) *textMatch {
	if tm == nil {
		return nil
	}

	encoded := &textMatch{
		Text:            tm.Text,
		NegateCondition: negateCondition(tm.NegateCondition),
	}
	return encoded
}

func encodeExpandRequest(e *CalendarExpandRequest) *expand {
	if e == nil {
		return nil
	}
	encoded := expand{
		Start: dateWithUTCTime(e.Start),
		End:   dateWithUTCTime(e.End),
	}
	return &encoded
}

func decodeCalendarObjectList(ms *internal.MultiStatus) ([]CalendarObject, error) {
	addrs := make([]CalendarObject, 0, len(ms.Responses))
	errs := make([]error, 0, len(ms.Responses))
	for _, resp := range ms.Responses {
		path, err := resp.Path()
		if err != nil {
			errs = append(errs, err)
			continue
		}

		var calData calendarDataResp
		if err := resp.DecodeProp(&calData); err != nil && !internal.IsNotFound(err) {
			return nil, err
		}

		// Skip responses without calendar-data (e.g., calendar collection itself)
		if len(calData.Data) == 0 {
			continue
		}

		var getLastMod internal.GetLastModified
		if err := resp.DecodeProp(&getLastMod); err != nil && !internal.IsNotFound(err) {
			return nil, err
		}

		var getETag internal.GetETag
		if err := resp.DecodeProp(&getETag); err != nil && !internal.IsNotFound(err) {
			return nil, err
		}

		var getContentLength internal.GetContentLength
		if err := resp.DecodeProp(&getContentLength); err != nil && !internal.IsNotFound(err) {
			return nil, err
		}

		r := bytes.NewReader(calData.Data)
		data, err := ical.NewDecoder(r).Decode()
		if err != nil {
			return nil, err
		}

		addrs = append(addrs, CalendarObject{
			Path:          path,
			ModTime:       time.Time(getLastMod.LastModified),
			ContentLength: getContentLength.Length,
			ETag:          string(getETag.ETag),
			Data:          data,
		})
	}

	return addrs, errors.Join(errs...)
}

// QueryCalendar queries calendar objects using a calendar-query REPORT.
//
// Options can be used to add filters:
//
//	opts := &caldav.QueryOptions{
//		TimeRangeStart: &start,
//		TimeRangeEnd:   &end,
//	}
//	client.QueryCalendar(ctx, path, query, opts)
//
// This automatically adds a time-range filter to the calendar query.
func (c *Client) QueryCalendar(ctx context.Context, calendar string, query *CalendarQuery, opts *QueryOptions) ([]CalendarObject, error) {
	modifiedQuery := *query

	if opts == nil {
		opts = &QueryOptions{}
	}

	if opts.TimeRangeStart != nil || opts.TimeRangeEnd != nil {
		for i := range modifiedQuery.CompFilter.Comps {
			if modifiedQuery.CompFilter.Comps[i].Name == "VEVENT" {
				modifiedQuery.CompFilter.Comps[i].Start = *opts.TimeRangeStart
				modifiedQuery.CompFilter.Comps[i].End = *opts.TimeRangeEnd
				break
			}
		}
	}

	propReq, err := encodeCalendarReq(&modifiedQuery.CompRequest)
	if err != nil {
		return nil, err
	}

	calendarQuery := calendarQuery{Prop: propReq}
	calendarQuery.Filter.CompFilter = *encodeCompFilter(&modifiedQuery.CompFilter)
	req, err := c.ic.NewXMLRequest("REPORT", calendar, &calendarQuery)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Depth", "1")

	ms, err := c.ic.DoMultiStatus(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	return decodeCalendarObjectList(ms)
}

func (c *Client) MultiGetCalendar(ctx context.Context, path string, multiGet *CalendarMultiGet) ([]CalendarObject, error) {
	propReq, err := encodeCalendarReq(&multiGet.CompRequest)
	if err != nil {
		return nil, err
	}

	calendarMultiget := calendarMultiget{Prop: propReq}

	if len(multiGet.Paths) == 0 {
		href := internal.Href{Path: path}
		calendarMultiget.Hrefs = []internal.Href{href}
	} else {
		calendarMultiget.Hrefs = make([]internal.Href, len(multiGet.Paths))
		for i, p := range multiGet.Paths {
			calendarMultiget.Hrefs[i] = internal.Href{Path: p}
		}
	}

	req, err := c.ic.NewXMLRequest("REPORT", path, &calendarMultiget)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Depth", "1")

	ms, err := c.ic.DoMultiStatus(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	return decodeCalendarObjectList(ms)
}

func populateCalendarObject(co *CalendarObject, h http.Header) error {
	if loc := h.Get("Location"); loc != "" {
		u, err := url.Parse(loc)
		if err != nil {
			return err
		}
		co.Path = u.Path
	}
	if etag := h.Get("ETag"); etag != "" {
		unquoted, err := strconv.Unquote(etag)
		if err != nil {
			// Fallback: some providers (Yandex, Mail.ru) return unquoted ETags
			co.ETag = etag
		} else {
			co.ETag = unquoted
		}
	}
	if contentLength := h.Get("Content-Length"); contentLength != "" {
		n, err := strconv.ParseInt(contentLength, 10, 64)
		if err != nil {
			return err
		}
		co.ContentLength = n
	}
	if lastModified := h.Get("Last-Modified"); lastModified != "" {
		t, err := http.ParseTime(lastModified)
		if err != nil {
			return err
		}
		co.ModTime = t
	}

	return nil
}

func (c *Client) GetCalendarObject(ctx context.Context, path string) (*CalendarObject, error) {
	req, err := c.ic.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", ical.MIMEType)

	resp, err := c.ic.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(mediaType, ical.MIMEType) {
		return nil, fmt.Errorf("caldav: expected Content-Type %q, got %q", ical.MIMEType, mediaType)
	}

	cal, err := ical.NewDecoder(resp.Body).Decode()
	if err != nil {
		return nil, err
	}

	co := &CalendarObject{
		Path: resp.Request.URL.Path,
		Data: cal,
	}
	if err := populateCalendarObject(co, resp.Header); err != nil {
		return nil, err
	}
	return co, nil
}

// PutCalendarObject creates or updates a calendar object on the server.
//
// The calendar object is sent as an iCalendar stream. The returned CalendarObject
// contains the server-generated ETag and other metadata.
//
// Options can be used to set conditional headers:
//
//	opts := &caldav.PutOptions{
//		IfMatch: webdav.ConditionalMatch("\"etag\""),
//	}
//	client.PutCalendarObject(ctx, path, cal, opts)
//
// If the server returns HTTP 412 (Precondition Failed), ErrPreconditionFailed is returned.
func (c *Client) PutCalendarObject(ctx context.Context, path string, cal *ical.Calendar, opts *PutOptions) (*CalendarObject, error) {
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return nil, err
	}

	req, err := c.ic.NewRequest(http.MethodPut, path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", ical.MIMEType)

	if opts == nil {
		opts = &PutOptions{}
	}

	if opts.IfMatch.IsSet() {
		req.Header.Set("If-Match", string(opts.IfMatch))
	}
	if opts.IfNoneMatch.IsSet() {
		req.Header.Set("If-None-Match", string(opts.IfNoneMatch))
	}

	resp, err := c.ic.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	// Handle precondition failed (HTTP 412)
	if resp.StatusCode == http.StatusPreconditionFailed {
		resp.Body.Close() // Close body before potential recursion
		// If conflict resolver is set, attempt automatic resolution
		if c.conflictResolver != nil {
			return c.handleConflict(ctx, path, cal)
		}
		// Otherwise return error for manual resolution
		return nil, ErrPreconditionFailed
	}

	// Only defer close after we know we won't return early
	defer resp.Body.Close()

	co := &CalendarObject{Path: path}
	if err := populateCalendarObject(co, resp.Header); err != nil {
		return nil, err
	}
	return co, nil
}

// handleConflict handles HTTP 412 Precondition Failed by applying conflict resolution strategy.
func (c *Client) handleConflict(ctx context.Context, path string, local *ical.Calendar) (*CalendarObject, error) {
	// Get current version from server
	remote, err := c.GetCalendarObject(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote version: %w", err)
	}

	// Create local object for comparison
	localObj := &CalendarObject{
		Path:    path,
		Data:    local,
		ModTime: time.Now(),
	}

	// Apply conflict resolution strategy
	decision := c.conflictResolver.Resolve(localObj, remote)

	// Execute action based on decision
	switch decision {
	case UseLocal:
		// Overwrite server version (without If-Match)
		return c.PutCalendarObject(ctx, path, local, nil)

	case UseRemote:
		// Use server version
		return remote, nil

	case Skip:
		// Return error for manual resolution
		return nil, ErrPreconditionFailed

	case Merge:
		// Not implemented yet
		return nil, ErrMergeNotSupported

	default:
		return nil, fmt.Errorf("caldav: unknown conflict decision: %v", decision)
	}
}

// DeleteCalendarObject deletes a calendar object from the server.
//
// This operation is idempotent - deleting a non-existent object returns success.
func (c *Client) DeleteCalendarObject(ctx context.Context, path string) error {
	req, err := c.ic.NewRequest(http.MethodDelete, path, nil)
	if err != nil {
		return err
	}

	resp, err := c.ic.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	resp.Body.Close()

	// Consider 404 Not Found as successful deletion (idempotent operation)
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}

	// Any 2xx status code indicates success
	if resp.StatusCode/100 == 2 {
		return nil
	}

	return &internal.HTTPError{Code: resp.StatusCode}
}

// SyncCalendar performs incremental synchronization using sync-collection REPORT (RFC 6578).
//
// It returns created, updated, and deleted events since the last sync-token.
// Use an empty syncToken for initial synchronization.
//
// If the sync-token has expired (HTTP 410 Gone), ErrSyncTokenExpired is returned.
//
// Example:
//
//	result, err := client.SyncCalendar(ctx, calendarPath, "")
//	if errors.Is(err, caldav.ErrSyncTokenExpired) {
//	    // Token expired, perform full sync
//	    result, err = client.SyncCalendar(ctx, calendarPath, "")
//	}
func (c *Client) SyncCalendar(ctx context.Context, calendar string, syncToken string, opts *SyncOptions) (*SyncResult, error) {
	syncLevel := internal.DepthOne
	if opts != nil && opts.SyncLevel != 0 {
		syncLevel = opts.SyncLevel
	}

	// Create properties to get ETag and calendar-data (for Yandex compatibility)
	prop, err := internal.EncodeProp(
		&calendarDataReq{Comp: &comp{Name: "VCALENDAR"}},
		internal.NewRawXMLElement(internal.GetETagName, nil, nil),
	)
	if err != nil {
		return nil, err
	}

	ms, err := c.ic.SyncCollection(ctx, calendar, syncToken, syncLevel, nil, prop)
	if err != nil {
		// Handle expired sync-token (HTTP 410 Gone)
		var httpErr *internal.HTTPError
		if errors.As(err, &httpErr) && httpErr.Code == http.StatusGone {
			return nil, ErrSyncTokenExpired
		}
		return nil, err
	}

	result := NewSyncResult(ms.SyncToken)

	for _, resp := range ms.Responses {
		path, err := resp.Path()
		if err != nil {
			continue
		}

		// Skip root calendar
		if path == calendar {
			continue
		}

		var status string
		var etag string
		var calendarData []byte

		// Extract status and data from response
		if resp.Status != nil {
			status = resp.Status.Text
		}

		// Extract properties from propstat
		for _, propstat := range resp.PropStats {
			if err := resp.Err(); err != nil {
				continue
			}

			// Extract ETag
			var getETag internal.GetETag
			if err := propstat.Prop.Decode(&getETag); err == nil {
				etag = string(getETag.ETag)
			}

			// Extract calendar-data if available
			var calData calendarDataResp
			if err := propstat.Prop.Decode(&calData); err == nil {
				calendarData = calData.Data
			}

			// Determine status from propstat
			if status == "" {
				status = propstat.Status.Text
			}
		}

		// Process events based on status
		if strings.Contains(status, "404") {
			// Event deleted
			uid := c.extractUIDFromPath(path)
			result.AddDeleted(path, uid)

		} else if strings.Contains(status, "200") && len(calendarData) > 0 {
			// Event created/updated (with calendar-data)
			r := bytes.NewReader(calendarData)
			cal, err := ical.NewDecoder(r).Decode()
			if err != nil {
				continue
			}

			obj := CalendarObject{
				Path:    path,
				ETag:    etag,
				Data:    cal,
				ModTime: time.Now(),
			}

			if strings.Contains(status, "201") {
				result.AddCreated(obj)
			} else {
				result.AddUpdated(obj)
			}

		} else if strings.Contains(status, "200") {
			// Event created/updated (without calendar-data - ETag only)
			obj := CalendarObject{
				Path: path,
				ETag: etag,
			}
			result.AddUpdated(obj)
		}
	}

	return result, nil
}

// extractUIDFromPath extracts event UID from CalDAV path
func (c *Client) extractUIDFromPath(path string) string {
	// Path usually looks like: /calendars/user/calendar-id/event-uid.ics
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return ""
	}

	filename := parts[len(parts)-1]
	// Remove .ics extension
	if strings.HasSuffix(filename, ".ics") {
		filename = filename[:len(filename)-4]
	}

	return filename
}
