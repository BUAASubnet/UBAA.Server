package features

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/BUAASubnet/UBAA.Server/internal/dto"
	"github.com/BUAASubnet/UBAA.Server/internal/httpx"
	"github.com/gofiber/fiber/v2"
)

var (
	ErrYgdkInvalidRequest  = errors.New("ygdk invalid request")
	ErrYgdkUnauthenticated = errors.New("ygdk unauthenticated")
	ErrYgdkUpstream        = errors.New("ygdk upstream error")
)

type ygdkPhotoUpload struct {
	Bytes    []byte
	FileName string
	MimeType string
}

type ygdkClockinSubmitRequest struct {
	ItemID        *int
	StartTime     *string
	EndTime       *string
	Place         *string
	ShareToSquare *bool
	Photo         *ygdkPhotoUpload
}

type ygdkContext struct {
	Classify    map[string]any
	Items       []map[string]any
	DefaultItem map[string]any
	Summary     dto.YgdkTermSummaryDto
	ExpiresAt   time.Time
}

func (s *Service) GetYgdkOverview(ctx context.Context, username string) (dto.YgdkOverviewResponse, error) {
	context, err := s.resolveYgdkContext(ctx, username)
	if err != nil {
		return dto.YgdkOverviewResponse{}, err
	}
	items := make([]dto.YgdkItemDto, 0, len(context.Items))
	for _, item := range context.Items {
		items = append(items, mapYgdkItem(item))
	}
	defaultID := ygdkIntPtr(context.DefaultItem, "item_id")
	defaultName := stringPtrNonBlank(ygdkString(context.DefaultItem, "name"))
	return dto.YgdkOverviewResponse{
		Summary:         context.Summary,
		ClassifyID:      ygdkInt(context.Classify, "classify_id"),
		ClassifyName:    ygdkString(context.Classify, "name"),
		DefaultItemID:   defaultID,
		DefaultItemName: defaultName,
		Items:           items,
	}, nil
}

func (s *Service) GetYgdkRecords(ctx context.Context, username string, page, size int) (dto.YgdkRecordsPageResponse, error) {
	if page <= 0 || size <= 0 {
		return dto.YgdkRecordsPageResponse{}, ErrYgdkInvalidRequest
	}
	context, err := s.resolveYgdkContext(ctx, username)
	if err != nil {
		return dto.YgdkRecordsPageResponse{}, err
	}
	raw, err := s.ygdkClient(username).Records(ctx, ygdkInt(context.Classify, "classify_id"), page, size)
	if err != nil {
		return dto.YgdkRecordsPageResponse{}, err
	}
	itemMap := map[int]map[string]any{}
	for _, item := range context.Items {
		itemMap[ygdkInt(item, "item_id")] = item
	}
	recordsRaw := ygdkSlice(raw["list"])
	records := make([]dto.YgdkRecordDto, 0, len(recordsRaw))
	for _, item := range recordsRaw {
		if record := ygdkMap(item); record != nil {
			records = append(records, mapYgdkRecord(record, itemMap))
		}
	}
	total := ygdkInt(raw, "total")
	return dto.YgdkRecordsPageResponse{
		Content: records,
		Total:   total,
		Page:    page,
		Size:    size,
		HasMore: page*size < total,
	}, nil
}

func (s *Service) SubmitYgdkClockin(ctx context.Context, username string, request ygdkClockinSubmitRequest) (dto.YgdkClockinSubmitResponse, error) {
	context, err := s.resolveYgdkContext(ctx, username)
	if err != nil {
		return dto.YgdkClockinSubmitResponse{}, err
	}
	item := context.DefaultItem
	if request.ItemID != nil {
		item = nil
		for _, candidate := range context.Items {
			if ygdkInt(candidate, "item_id") == *request.ItemID {
				item = candidate
				break
			}
		}
		if item == nil {
			return dto.YgdkClockinSubmitResponse{}, ErrYgdkInvalidRequest
		}
	}
	startAt, endAt, err := resolveYgdkTimeRange(request.StartTime, request.EndTime, time.Now())
	if err != nil {
		return dto.YgdkClockinSubmitResponse{}, err
	}
	place := "操场"
	if request.Place != nil && strings.TrimSpace(*request.Place) != "" {
		place = strings.TrimSpace(*request.Place)
	}
	share := false
	if request.ShareToSquare != nil {
		share = *request.ShareToSquare
	}
	upload := request.Photo
	if upload == nil {
		generatedBytes, err := generateYgdkTransparentPNG()
		if err != nil {
			return dto.YgdkClockinSubmitResponse{}, err
		}
		upload = &ygdkPhotoUpload{Bytes: generatedBytes, FileName: "ygdk_auto_" + strconv.FormatInt(time.Now().UnixMilli(), 10) + ".png", MimeType: "image/png"}
	}
	client := s.ygdkClient(username)
	file, err := client.UploadImage(ctx, *upload)
	if err != nil {
		return dto.YgdkClockinSubmitResponse{}, err
	}
	result, err := client.Clockin(ctx, ygdkInt(context.Classify, "classify_id"), ygdkInt(item, "item_id"), ygdkString(item, "name"), startAt, endAt, place, ygdkString(file, "file_name"), share)
	if err != nil {
		return dto.YgdkClockinSubmitResponse{}, err
	}
	s.ygdkContexts.Delete(username)
	summary := mergeYgdkSubmitSummary(context.Summary, result)
	return dto.YgdkClockinSubmitResponse{Success: true, Message: "打卡成功", RecordID: ygdkIntPtr(result, "record_id"), Summary: &summary}, nil
}

func (s *Service) resolveYgdkContext(ctx context.Context, username string) (*ygdkContext, error) {
	if raw, ok := s.ygdkContexts.Load(username); ok {
		cached := raw.(*ygdkContext)
		if time.Now().Before(cached.ExpiresAt) {
			return cached, nil
		}
	}
	client := s.ygdkClient(username)
	classifies, err := client.ClassifyList(ctx)
	if err != nil {
		return nil, err
	}
	classify := resolveYgdkSportsClassify(classifies)
	if classify == nil {
		return nil, ErrYgdkUpstream
	}
	items, err := client.ItemList(ctx, ygdkInt(classify, "classify_id"))
	if err != nil {
		return nil, err
	}
	defaultItem := resolveYgdkDefaultItem(items)
	if defaultItem == nil {
		return nil, ErrYgdkUpstream
	}
	count, _ := client.CheckCount(ctx, ygdkInt(classify, "classify_id"))
	term, _ := client.Term(ctx)
	cached := &ygdkContext{
		Classify:    classify,
		Items:       items,
		DefaultItem: defaultItem,
		Summary:     mapYgdkSummary(classify, count, term),
		ExpiresAt:   time.Now().Add(time.Minute),
	}
	s.ygdkContexts.Store(username, cached)
	return cached, nil
}

func (s *Service) ygdkClient(username string) *ygdkClient {
	now := time.Now()
	if raw, ok := s.ygdkClients.Load(username); ok {
		cached := raw.(*cachedYgdkClient)
		cached.lastAccessAt = now
		return cached.client
	}
	client := &ygdkClient{username: username, service: s}
	cached := &cachedYgdkClient{client: client, lastAccessAt: now}
	actual, _ := s.ygdkClients.LoadOrStore(username, cached)
	return actual.(*cachedYgdkClient).client
}

type ygdkClient struct {
	username string
	service  *Service
	uid      int
	token    string
}

func (c *ygdkClient) ClassifyList(ctx context.Context) ([]map[string]any, error) {
	raw, err := c.postForm(ctx, "get_classify_list", "/Front/Clockin/Classify/getList", nil, nil)
	if err != nil {
		return nil, err
	}
	return ygdkMapSlice(ygdkMap(raw)["list"]), nil
}

func (c *ygdkClient) ItemList(ctx context.Context, classifyID int) ([]map[string]any, error) {
	params := url.Values{"page": {"1"}, "limit": {"1000"}, "classify_id": {strconv.Itoa(classifyID)}}
	raw, err := c.postForm(ctx, "get_item_list", "/Front/Clockin/Item/getList", params, params)
	if err != nil {
		return nil, err
	}
	return ygdkMapSlice(ygdkMap(raw)["list"]), nil
}

func (c *ygdkClient) CheckCount(ctx context.Context, classifyID int) (map[string]any, error) {
	if err := c.ensureLogin(ctx, false); err != nil {
		return nil, err
	}
	form := url.Values{"classify_id": {strconv.Itoa(classifyID)}, "user_id": {strconv.Itoa(c.uid)}}
	raw, err := c.postForm(ctx, "get_check_count", "/Front/Clockin/Clockin/getCount", nil, form)
	if err != nil {
		return nil, err
	}
	return ygdkMap(raw), nil
}

func (c *ygdkClient) Term(ctx context.Context) (map[string]any, error) {
	raw, err := c.postForm(ctx, "get_term", "/Front/Clockin/Term/get", nil, nil)
	if err != nil {
		return nil, err
	}
	return ygdkMap(raw), nil
}

func (c *ygdkClient) Records(ctx context.Context, classifyID, page, limit int) (map[string]any, error) {
	if err := c.ensureLogin(ctx, false); err != nil {
		return nil, err
	}
	form := url.Values{
		"page":        {strconv.Itoa(page)},
		"limit":       {strconv.Itoa(limit)},
		"classify_id": {strconv.Itoa(classifyID)},
		"user_id":     {strconv.Itoa(c.uid)},
	}
	raw, err := c.postForm(ctx, "get_records", "/Front/Clockin/Clockin/getList", nil, form)
	if err != nil {
		return nil, err
	}
	return ygdkMap(raw), nil
}

func (c *ygdkClient) UploadImage(ctx context.Context, photo ygdkPhotoUpload) (map[string]any, error) {
	if err := c.ensureLogin(ctx, false); err != nil {
		return nil, err
	}
	sessionClient, err := c.service.clients.Get(c.username)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("uid", strconv.Itoa(c.uid))
	_ = writer.WriteField("token", c.token)
	part, err := writer.CreateFormFile("file", photo.FileName)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(photo.Bytes); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.service.clients.UpstreamURL(ygdkAPIBase+"/Front/Upload/File/post"), &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := sessionClient.Client.Do(req)
	if err != nil {
		return nil, upstreamRequestError(err, ErrYgdkUpstream, "ygdk_timeout")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	result, err := c.unwrapResponse(raw)
	if err != nil {
		return nil, err
	}
	file := ygdkMap(result)
	if ygdkString(file, "file_name") == "" {
		return nil, ErrYgdkUpstream
	}
	return file, nil
}

func (c *ygdkClient) Clockin(ctx context.Context, classifyID, itemID int, itemName string, startAt, endAt time.Time, place, imageName string, isOpen bool) (map[string]any, error) {
	form := url.Values{
		"start_time":    {strconv.FormatInt(startAt.Unix(), 10)},
		"end_time":      {strconv.FormatInt(endAt.Unix(), 10)},
		"place_type":    {"1"},
		"place":         {place},
		"isopen":        {boolYgdkIntString(isOpen)},
		"form_time_fmt": {formatYgdkFormTime(startAt, endAt)},
		"images":        {`["` + imageName + `"]`},
		"classify_id":   {strconv.Itoa(classifyID)},
		"item_id":       {strconv.Itoa(itemID)},
		"item_name":     {itemName},
	}
	raw, err := c.postForm(ctx, "clockin", "/Front/Clockin/Clockin/clockin", nil, form)
	if err != nil {
		return nil, err
	}
	return ygdkMap(raw), nil
}

func (c *ygdkClient) ensureLogin(ctx context.Context, force bool) error {
	if !force && c.uid != 0 && strings.TrimSpace(c.token) != "" {
		return nil
	}
	code, err := c.fetchOAuthCode(ctx)
	if err != nil {
		return err
	}
	return c.exchangeCode(ctx, code)
}

func (c *ygdkClient) fetchOAuthCode(ctx context.Context) (string, error) {
	client, err := c.service.clients.NewNoRedirect(c.username)
	if err != nil {
		return "", err
	}
	currentURL := ygdkOAuthURL()
	for range 10 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.service.clients.UpstreamURL(currentURL), nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", upstreamRequestError(err, ErrYgdkUpstream, "ygdk_timeout")
		}
		if code := extractYgdkCode(resp.Request.URL.String()); code != "" {
			_ = resp.Body.Close()
			return code, nil
		}
		location := resp.Header.Get("Location")
		_ = resp.Body.Close()
		next := resolveFeatureLocation(resp.Request.URL, location)
		if code := extractYgdkCode(next); code != "" {
			return code, nil
		}
		if location == "" {
			break
		}
		currentURL = next
	}
	return "", ErrYgdkUnauthenticated
}

func (c *ygdkClient) exchangeCode(ctx context.Context, code string) error {
	sessionClient, err := c.service.clients.Get(c.username)
	if err != nil {
		return err
	}
	target := c.service.clients.UpstreamURL(ygdkAPIBase + "/Front/Clockin/User/campusAppLogin?code=" + url.QueryEscape(code))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := sessionClient.Client.Do(req)
	if err != nil {
		return upstreamRequestError(err, ErrYgdkUpstream, "ygdk_timeout")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	result, err := c.unwrapResponse(raw)
	if err != nil {
		return err
	}
	data := ygdkMap(ygdkMap(result)["data"])
	if data == nil {
		data = ygdkMap(result)
	}
	uid := ygdkInt(data, "uid")
	token := ygdkString(data, "token")
	if decoded, err := url.QueryUnescape(token); err == nil {
		token = decoded
	}
	if uid == 0 || token == "" {
		return ErrYgdkUnauthenticated
	}
	c.uid = uid
	c.token = token
	return nil
}

func (c *ygdkClient) postForm(ctx context.Context, operation, path string, queryParameters, formParameters url.Values) (any, error) {
	if err := c.ensureLogin(ctx, false); err != nil {
		return nil, err
	}
	sessionClient, err := c.service.clients.Get(c.username)
	if err != nil {
		return nil, err
	}
	bodyValues := url.Values{}
	for key, values := range formParameters {
		for _, value := range values {
			bodyValues.Add(key, value)
		}
	}
	bodyValues.Set("uid", strconv.Itoa(c.uid))
	bodyValues.Set("token", c.token)
	targetURL, _ := url.Parse(c.service.clients.UpstreamURL(ygdkAPIBase + path))
	query := targetURL.Query()
	for key, values := range queryParameters {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	targetURL.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL.String(), strings.NewReader(bodyValues.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	resp, err := sessionClient.Client.Do(req)
	if err != nil {
		return nil, upstreamRequestError(err, ErrYgdkUpstream, "ygdk_timeout")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return c.unwrapResponse(raw)
}

func (c *ygdkClient) unwrapResponse(raw []byte) (any, error) {
	payload := map[string]any{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, ErrYgdkUpstream
	}
	switch ygdkInt(payload, "code") {
	case 1:
		if result, ok := payload["result"]; ok {
			return result, nil
		}
		return nil, nil
	case -98:
		c.uid = 0
		c.token = ""
		return nil, ErrYgdkUnauthenticated
	default:
		return nil, ygdkMessage(ErrYgdkUpstream, firstNonBlank(ygdkString(payload, "msg"), "阳光打卡请求失败"))
	}
}

func parseYgdkClockinRequest(c *fiber.Ctx) (ygdkClockinSubmitRequest, error) {
	form, err := c.MultipartForm()
	if err != nil {
		return ygdkClockinSubmitRequest{}, err
	}
	request := ygdkClockinSubmitRequest{}
	if value := form.Value["itemId"]; len(value) > 0 {
		if parsed, err := strconv.Atoi(value[0]); err == nil {
			request.ItemID = &parsed
		}
	}
	if value := form.Value["startTime"]; len(value) > 0 {
		text := value[0]
		request.StartTime = &text
	}
	if value := form.Value["endTime"]; len(value) > 0 {
		text := value[0]
		request.EndTime = &text
	}
	if value := form.Value["place"]; len(value) > 0 {
		text := value[0]
		request.Place = &text
	}
	if value := form.Value["shareToSquare"]; len(value) > 0 {
		if parsed, err := strconv.ParseBool(value[0]); err == nil {
			request.ShareToSquare = &parsed
		}
	}
	if files := form.File["photo"]; len(files) > 0 {
		file := files[0]
		opened, err := file.Open()
		if err != nil {
			return ygdkClockinSubmitRequest{}, err
		}
		defer opened.Close()
		data, err := io.ReadAll(opened)
		if err != nil {
			return ygdkClockinSubmitRequest{}, err
		}
		if len(data) > 0 {
			mimeType := file.Header.Get("Content-Type")
			if mimeType == "" {
				mimeType = "application/octet-stream"
			}
			request.Photo = &ygdkPhotoUpload{Bytes: data, FileName: firstNonBlank(file.Filename, "upload.jpg"), MimeType: mimeType}
		}
	}
	return request, nil
}

func resolveYgdkSportsClassify(classifies []map[string]any) map[string]any {
	if len(classifies) == 0 {
		return nil
	}
	for _, classify := range classifies {
		if strings.Contains(ygdkString(classify, "name"), "体育") {
			return classify
		}
	}
	for _, classify := range classifies {
		if ygdkInt(classify, "classify_id") == 1 {
			return classify
		}
	}
	return classifies[0]
}

func resolveYgdkDefaultItem(items []map[string]any) map[string]any {
	if len(items) == 0 {
		return nil
	}
	for _, item := range items {
		if strings.Contains(ygdkString(item, "name"), "跑") {
			return item
		}
	}
	best := items[0]
	bestSort := ygdkIntDefault(best, "sort", int(^uint(0)>>1))
	for _, item := range items[1:] {
		if sortValue := ygdkIntDefault(item, "sort", int(^uint(0)>>1)); sortValue < bestSort {
			best = item
			bestSort = sortValue
		}
	}
	return best
}

func mapYgdkSummary(classify, count, term map[string]any) dto.YgdkTermSummaryDto {
	return dto.YgdkTermSummaryDto{
		TermID:      firstYgdkIntPtr(ygdkIntPtr(term, "term_id"), ygdkIntPtr(term, "id")),
		TermName:    stringPtrNonBlank(ygdkString(term, "name")),
		TermCount:   firstYgdkIntValue(ygdkIntPtr(count, "term_good_count_show"), ygdkIntPtr(count, "term_good_count"), ygdkIntPtr(count, "term_count_show"), ygdkIntPtr(count, "term_count"), intPtr(0)),
		TermTarget:  firstYgdkIntPtr(ygdkIntPtr(count, "term_num"), ygdkIntPtr(classify, "term_num")),
		WeekCount:   ygdkIntPtr(count, "week_count"),
		WeekTarget:  firstYgdkIntPtr(ygdkIntPtr(count, "week_num"), ygdkIntPtr(classify, "week_num")),
		MonthCount:  ygdkIntPtr(count, "month_count"),
		MonthTarget: firstYgdkIntPtr(ygdkIntPtr(count, "month_num"), ygdkIntPtr(classify, "month_num")),
		DayCount:    ygdkIntPtr(count, "day_count"),
		GoodCount:   firstYgdkIntPtr(ygdkIntPtr(count, "term_good_count_show"), ygdkIntPtr(count, "term_good_count")),
	}
}

func mergeYgdkSubmitSummary(existing dto.YgdkTermSummaryDto, result map[string]any) dto.YgdkTermSummaryDto {
	existing.TermID = firstYgdkIntPtr(ygdkIntPtr(result, "term_id"), existing.TermID)
	existing.TermCount = firstYgdkIntValue(ygdkIntPtr(result, "term_good_count_show"), ygdkIntPtr(result, "term_good_count"), ygdkIntPtr(result, "term_count_show"), ygdkIntPtr(result, "term_count"), &existing.TermCount)
	existing.TermTarget = firstYgdkIntPtr(ygdkIntPtr(result, "term_num"), existing.TermTarget)
	existing.WeekCount = firstYgdkIntPtr(ygdkIntPtr(result, "week_count"), existing.WeekCount)
	existing.WeekTarget = firstYgdkIntPtr(ygdkIntPtr(result, "week_num"), existing.WeekTarget)
	existing.MonthCount = firstYgdkIntPtr(ygdkIntPtr(result, "month_count"), existing.MonthCount)
	existing.MonthTarget = firstYgdkIntPtr(ygdkIntPtr(result, "month_num"), existing.MonthTarget)
	existing.DayCount = firstYgdkIntPtr(ygdkIntPtr(result, "day_count"), existing.DayCount)
	existing.GoodCount = firstYgdkIntPtr(ygdkIntPtr(result, "term_good_count_show"), ygdkIntPtr(result, "term_good_count"), existing.GoodCount)
	return existing
}

func mapYgdkItem(raw map[string]any) dto.YgdkItemDto {
	return dto.YgdkItemDto{ItemID: ygdkInt(raw, "item_id"), Name: ygdkString(raw, "name"), Type: ygdkIntPtr(raw, "type"), Sort: ygdkIntPtr(raw, "sort")}
}

func mapYgdkRecord(raw map[string]any, itemMap map[int]map[string]any) dto.YgdkRecordDto {
	itemID := ygdkIntPtr(raw, "item_id")
	itemName := stringPtrNonBlank(ygdkString(raw, "item_name"))
	if itemName == nil && itemID != nil {
		itemName = stringPtrNonBlank(ygdkString(itemMap[*itemID], "name"))
	}
	return dto.YgdkRecordDto{
		RecordID:       ygdkInt(raw, "record_id"),
		ItemID:         itemID,
		ItemName:       itemName,
		StartTime:      ygdkTimestampToText(ygdkInt64Ptr(raw, "start_time")),
		EndTime:        ygdkTimestampToText(ygdkInt64Ptr(raw, "end_time")),
		Place:          stringPtrNonBlank(ygdkString(raw, "place")),
		Images:         extractYgdkImages(raw),
		IsOpen:         ygdkInt(raw, "isopen") == 1,
		State:          ygdkIntPtr(raw, "state"),
		CreatedAt:      stringPtrNonBlank(ygdkString(raw, "create_time_fmt")),
		CreatedAtLabel: stringPtrNonBlank(ygdkString(raw, "create_time_fmt")),
	}
}

func resolveYgdkTimeRange(startTime, endTime *string, now time.Time) (time.Time, time.Time, error) {
	startRaw := ""
	endRaw := ""
	if startTime != nil {
		startRaw = strings.TrimSpace(*startTime)
	}
	if endTime != nil {
		endRaw = strings.TrimSpace(*endTime)
	}
	if (startRaw == "") != (endRaw == "") {
		return time.Time{}, time.Time{}, ErrYgdkInvalidRequest
	}
	if startRaw == "" {
		return generateDefaultYgdkTimeRange(now)
	}
	startAt, err := parseYgdkDateTime(startRaw)
	if err != nil {
		return time.Time{}, time.Time{}, ErrYgdkInvalidRequest
	}
	endAt, err := parseYgdkDateTime(endRaw)
	if err != nil || !endAt.After(startAt) || !sameYgdkDay(startAt, endAt) {
		return time.Time{}, time.Time{}, ErrYgdkInvalidRequest
	}
	return startAt, endAt, nil
}

func parseYgdkDateTime(value string) (time.Time, error) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	if parsed, err := time.ParseInLocation("2006-01-02 15:04", value, loc); err == nil {
		return parsed, nil
	}
	return time.ParseInLocation("2006-01-02T15:04:05", value, loc)
}

func generateDefaultYgdkTimeRange(now time.Time) (time.Time, time.Time, error) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	now = now.In(loc)
	candidates := []time.Time{}
	for offset := 0; offset < 3; offset++ {
		date := now.AddDate(0, 0, -offset)
		dayStart := time.Date(date.Year(), date.Month(), date.Day(), 8, 0, 0, 0, loc)
		latestEnd := time.Date(date.Year(), date.Month(), date.Day(), 22, 0, 0, 0, loc)
		if offset == 0 && now.Before(latestEnd) {
			latestEnd = now
		}
		latestStart := latestEnd.Add(-time.Hour)
		for start := dayStart; !start.After(latestStart); start = start.Add(time.Hour) {
			candidates = append(candidates, start)
		}
	}
	if len(candidates) == 0 {
		start := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, loc)
		return start, start.Add(time.Hour), nil
	}
	start := candidates[int(time.Now().UnixNano()%int64(len(candidates)))]
	return start, start.Add(time.Hour), nil
}

func generateYgdkTransparentPNG() ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, 640, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			img.Set(x, y, color.NRGBA{A: 0})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func ygdkError(c *fiber.Ctx, err error) error {
	if code, ok := timeoutCode(err); ok {
		return httpx.Error(c, fiber.StatusGatewayTimeout, code)
	}
	switch {
	case errors.Is(err, ErrYgdkInvalidRequest):
		return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
	case errors.Is(err, ErrYgdkUnauthenticated):
		return httpx.Error(c, fiber.StatusUnauthorized, "unauthenticated")
	default:
		return httpx.Error(c, fiber.StatusBadGateway, "ygdk_error")
	}
}

func ygdkMessage(err error, message string) error {
	return fmt.Errorf("%w: %s", err, message)
}

func ygdkOAuthURL() string {
	redirect := url.QueryEscape(ygdkFrontBase + "/#/home")
	return "https://app.buaa.edu.cn/uc/api/oauth/index?redirect=" + redirect + "&appid=" + ygdkAppID + "&state=STATE&qrcode=1"
}

func extractYgdkCode(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	for _, query := range []string{parsed.RawQuery, ""} {
		if query == "" && parsed.Fragment != "" {
			if index := strings.Index(parsed.Fragment, "?"); index >= 0 {
				query = parsed.Fragment[index+1:]
			}
		}
		values, _ := url.ParseQuery(query)
		if code := values.Get("code"); code != "" {
			return code
		}
	}
	return ""
}

func ygdkTimestampToText(value *int64) *string {
	if value == nil {
		return nil
	}
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	text := time.Unix(*value, 0).In(loc).Format("2006-01-02 15:04")
	return &text
}

func extractYgdkImages(raw map[string]any) []string {
	formatted := raw["images_fmt"]
	if items := ygdkStringList(formatted); len(items) > 0 {
		return items
	}
	rawImages := ygdkString(raw, "images")
	if rawImages == "" {
		return []string{}
	}
	var parsed []string
	if err := json.Unmarshal([]byte(rawImages), &parsed); err == nil {
		return parsed
	}
	return []string{}
}

func formatYgdkFormTime(startAt, endAt time.Time) string {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	return startAt.In(loc).Format("2006-01-02 15:04") + "-" + endAt.In(loc).Format("15:04")
}

func sameYgdkDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func boolYgdkIntString(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func firstYgdkIntPtr(values ...*int) *int {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstYgdkIntValue(values ...*int) int {
	for _, value := range values {
		if value != nil {
			return *value
		}
	}
	return 0
}

func ygdkMapSlice(value any) []map[string]any {
	raw := ygdkSlice(value)
	result := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if mapped := ygdkMap(item); mapped != nil {
			result = append(result, mapped)
		}
	}
	return result
}

func ygdkMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func ygdkSlice(value any) []any {
	if typed, ok := value.([]any); ok {
		return typed
	}
	return nil
}

func ygdkString(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	return ygdkAnyString(raw[key])
}

func ygdkAnyString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func ygdkStringList(value any) []string {
	switch typed := value.(type) {
	case []any:
		result := []string{}
		for _, item := range typed {
			if text := ygdkAnyString(item); text != "" {
				result = append(result, text)
			}
		}
		return result
	case string:
		if strings.TrimSpace(typed) != "" {
			return []string{strings.TrimSpace(typed)}
		}
	}
	return nil
}

func ygdkInt(raw map[string]any, key string) int {
	return ygdkIntAny(raw[key], 0)
}

func ygdkIntDefault(raw map[string]any, key string, fallback int) int {
	return ygdkIntAny(raw[key], fallback)
}

func ygdkIntAny(value any, fallback int) int {
	switch typed := value.(type) {
	case nil:
		return fallback
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return fallback
}

func ygdkIntPtr(raw map[string]any, key string) *int {
	if raw == nil || raw[key] == nil {
		return nil
	}
	value := ygdkInt(raw, key)
	return &value
}

func ygdkInt64Ptr(raw map[string]any, key string) *int64 {
	if raw == nil || raw[key] == nil {
		return nil
	}
	switch typed := raw[key].(type) {
	case int64:
		return &typed
	case float64:
		value := int64(typed)
		return &value
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
			return &parsed
		}
	}
	return nil
}

const (
	ygdkFrontBase = "https://ygdk.buaa.edu.cn"
	ygdkAPIBase   = ygdkFrontBase + "/api"
	ygdkAppID     = "200230221144501510"
)
