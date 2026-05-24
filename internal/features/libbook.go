package features

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BUAASubnet/UBAA.Server/internal/dto"
	"github.com/BUAASubnet/UBAA.Server/internal/httpx"
	"github.com/BUAASubnet/UBAA.Server/internal/upstream"
	"github.com/gofiber/fiber/v2"
)

var (
	ErrLibBookAuth            = errors.New("libbook auth failed")
	ErrLibBookInvalidRequest  = errors.New("libbook invalid request")
	ErrLibBookNotFound        = errors.New("libbook not found")
	ErrLibBookSeatUnavailable = errors.New("libbook seat unavailable")
	ErrLibBookUpstream        = errors.New("libbook upstream error")
)

type libBookErrorWithMessage struct {
	err     error
	message string
}

func (e libBookErrorWithMessage) Error() string {
	return e.message
}

func (e libBookErrorWithMessage) Unwrap() error {
	return e.err
}

func (s *Service) GetLibBookLibraries(ctx context.Context, username, day string) ([]dto.LibBookLibraryDto, error) {
	client := s.libBookClient(username)
	items, err := client.GetLibraries(ctx, day)
	if err != nil {
		return nil, err
	}
	libraries := make([]dto.LibBookLibraryDto, 0, len(items))
	for _, item := range items {
		libraries = append(libraries, mapLibBookLibrary(item))
	}
	return libraries, nil
}

func (s *Service) GetLibBookAreas(ctx context.Context, username, premisesID, storeyID, day string) ([]dto.LibBookAreaDto, error) {
	if strings.TrimSpace(premisesID) == "" {
		return nil, libBookMessage(ErrLibBookInvalidRequest, "缺少楼馆参数")
	}
	client := s.libBookClient(username)
	items, err := client.GetAreas(ctx, premisesID, storeyID, day)
	if err != nil {
		return nil, err
	}
	areas := make([]dto.LibBookAreaDto, 0, len(items))
	for _, item := range items {
		areas = append(areas, mapLibBookArea(item))
	}
	return areas, nil
}

func (s *Service) GetLibBookAreaDetail(ctx context.Context, username, areaID string) (dto.LibBookAreaDetailDto, error) {
	if strings.TrimSpace(areaID) == "" {
		return dto.LibBookAreaDetailDto{}, libBookMessage(ErrLibBookInvalidRequest, "缺少分区参数")
	}
	raw, err := s.libBookClient(username).GetAreaInfo(ctx, areaID)
	if err != nil {
		return dto.LibBookAreaDetailDto{}, err
	}
	return mapLibBookAreaDetail(areaID, raw), nil
}

func (s *Service) GetLibBookSeats(ctx context.Context, username, areaID, day, startTime, endTime string) ([]dto.LibBookSeatDto, error) {
	if strings.TrimSpace(areaID) == "" || strings.TrimSpace(day) == "" {
		return nil, libBookMessage(ErrLibBookInvalidRequest, "缺少座位查询参数")
	}
	items, err := s.libBookClient(username).GetSeats(ctx, areaID, day, startTime, endTime)
	if err != nil {
		return nil, err
	}
	seats := make([]dto.LibBookSeatDto, 0, len(items))
	for _, item := range items {
		seats = append(seats, mapLibBookSeat(item))
	}
	sort.SliceStable(seats, func(i, j int) bool { return seats[i].No < seats[j].No })
	return seats, nil
}

func (s *Service) ReserveLibBookSeat(ctx context.Context, username string, request dto.LibBookReserveRequest) (dto.LibBookReserveResponse, error) {
	if strings.TrimSpace(request.AreaID) == "" {
		return dto.LibBookReserveResponse{}, libBookMessage(ErrLibBookInvalidRequest, "缺少分区参数")
	}
	if strings.TrimSpace(request.SeatID) == "" {
		return dto.LibBookReserveResponse{}, libBookMessage(ErrLibBookInvalidRequest, "请选择座位")
	}
	if strings.TrimSpace(request.Day) == "" || strings.TrimSpace(request.Segment) == "" {
		return dto.LibBookReserveResponse{}, libBookMessage(ErrLibBookInvalidRequest, "预约时间段无效，请刷新后重试")
	}
	raw, err := s.libBookClient(username).Reserve(ctx, request)
	if err != nil {
		return dto.LibBookReserveResponse{}, err
	}
	message := libBookMessageOrDefault(raw, "预约成功")
	if !libBookBusinessSuccess(raw) {
		return dto.LibBookReserveResponse{}, libBookMessage(libBookMessageToError(message), message)
	}
	response := dto.LibBookReserveResponse{Success: true, Message: message}
	if data := libBookMap(raw["data"]); data != nil {
		if bookInfo := libBookMap(data["bookInfo"]); bookInfo != nil {
			booking := mapLibBookBooking(bookInfo)
			response.Booking = &booking
		} else if bookingRaw := libBookMap(data["booking"]); bookingRaw != nil {
			booking := mapLibBookBooking(bookingRaw)
			response.Booking = &booking
		}
	}
	return response, nil
}

func (s *Service) GetLibBookBookings(ctx context.Context, username string, page, limit int) (dto.LibBookBookingsResponse, error) {
	raw, err := s.libBookClient(username).GetBookings(ctx, page, limit)
	if err != nil {
		return dto.LibBookBookingsResponse{}, err
	}
	data := libBookMap(raw["data"])
	items := []any{}
	if data != nil {
		items = libBookSlice(data["data"])
		if len(items) == 0 {
			items = libBookSlice(data["list"])
		}
	}
	bookings := make([]dto.LibBookBookingDto, 0, len(items))
	for _, item := range items {
		if rawBooking := libBookMap(item); rawBooking != nil {
			bookings = append(bookings, mapLibBookBooking(rawBooking))
		}
	}
	return dto.LibBookBookingsResponse{
		Bookings: bookings,
		Page:     libBookIntAny(libBookMapValue(data, "current_page"), libBookIntAny(libBookMapValue(data, "page"), page)),
		Limit:    libBookIntAny(libBookMapValue(data, "per_page"), libBookIntAny(libBookMapValue(data, "limit"), limit)),
		Total:    libBookIntAny(libBookMapValue(data, "total"), len(bookings)),
	}, nil
}

func (s *Service) CancelLibBookBooking(ctx context.Context, username, bookingID string) (dto.LibBookCancelResponse, error) {
	if strings.TrimSpace(bookingID) == "" {
		return dto.LibBookCancelResponse{}, libBookMessage(ErrLibBookNotFound, "预约记录不存在或已失效")
	}
	raw, err := s.libBookClient(username).CancelBooking(ctx, bookingID)
	if err != nil {
		return dto.LibBookCancelResponse{}, err
	}
	message := libBookMessageOrDefault(raw, "取消成功")
	if !libBookBusinessSuccess(raw) {
		return dto.LibBookCancelResponse{}, libBookMessage(libBookMessageToError(message), message)
	}
	return dto.LibBookCancelResponse{Success: true, Message: message}, nil
}

func (s *Service) libBookClient(username string) *libBookClient {
	now := time.Now()
	if raw, ok := s.libBookClients.Load(username); ok {
		cached := raw.(*cachedLibBookClient)
		cached.lastAccessAt = now
		return cached.client
	}
	client := &libBookClient{username: username, service: s}
	cached := &cachedLibBookClient{client: client, lastAccessAt: now}
	actual, _ := s.libBookClients.LoadOrStore(username, cached)
	return actual.(*cachedLibBookClient).client
}

type libBookClient struct {
	username string
	service  *Service
	token    string
}

func (c *libBookClient) GetLibraries(ctx context.Context, day string) ([]any, error) {
	raw, err := c.requestJSON(ctx, "list_libraries", "space/pcTopFor", map[string]any{"day": day}, true, true)
	if err != nil {
		return nil, err
	}
	return libBookSlice(libBookMap(raw["data"])["list"]), nil
}

func (c *libBookClient) GetAreas(ctx context.Context, premisesID, storeyID, day string) ([]any, error) {
	storeys := []any{}
	if strings.TrimSpace(storeyID) != "" {
		storeys = append(storeys, storeyID)
	}
	raw, err := c.requestJSON(ctx, "list_areas", "space/pick", map[string]any{
		"premisesIds": premisesID,
		"categoryIds": []any{},
		"storeyIds":   storeys,
		"boutiqueIds": []any{},
		"date":        day,
	}, true, true)
	if err != nil {
		return nil, err
	}
	return libBookSlice(libBookMap(raw["data"])["area"]), nil
}

func (c *libBookClient) GetAreaInfo(ctx context.Context, areaID string) (map[string]any, error) {
	return c.requestJSON(ctx, "get_area_info", "Space/map", map[string]any{"id": areaID}, true, true)
}

func (c *libBookClient) GetSeats(ctx context.Context, areaID, day, startTime, endTime string) ([]any, error) {
	raw, err := c.requestJSON(ctx, "list_seats", "Space/seat", map[string]any{
		"id":         areaID,
		"day":        day,
		"label_id":   []any{},
		"start_time": startTime,
		"end_time":   endTime,
		"begdate":    "",
		"enddate":    "",
	}, true, true)
	if err != nil {
		return nil, err
	}
	return libBookSlice(libBookMap(raw["data"])["list"]), nil
}

func (c *libBookClient) Reserve(ctx context.Context, request dto.LibBookReserveRequest) (map[string]any, error) {
	return c.requestJSON(ctx, "reserve", "space/confirm", map[string]any{"aesjson": encryptLibBookReserveRequest(request)}, true, true)
}

func (c *libBookClient) GetBookings(ctx context.Context, page, limit int) (map[string]any, error) {
	return c.requestJSON(ctx, "list_bookings", "member/seat", map[string]any{"type": "1", "page": page, "limit": limit}, true, true)
}

func (c *libBookClient) CancelBooking(ctx context.Context, bookingID string) (map[string]any, error) {
	return c.requestJSON(ctx, "cancel_booking", "space/cancel", map[string]any{"id": bookingID}, true, true)
}

func (c *libBookClient) ensureLogin(ctx context.Context, force bool) error {
	if !force && strings.TrimSpace(c.token) != "" {
		return nil
	}
	c.token = ""
	cas, err := c.fetchCasToken(ctx)
	if err != nil {
		return err
	}
	raw, err := c.requestJSON(ctx, "login", "login/user", map[string]any{"cas": cas}, false, false)
	if err != nil {
		return err
	}
	member := libBookMap(libBookMap(raw["data"])["member"])
	token := libBookString(member, "token")
	if token == "" {
		return ErrLibBookAuth
	}
	c.token = token
	return nil
}

func (c *libBookClient) fetchCasToken(ctx context.Context) (string, error) {
	client, err := c.service.clients.NewNoRedirect(c.username)
	if err != nil {
		return "", err
	}
	currentURL := c.service.clients.UpstreamURL(libBookCASLoginURL)
	for range 8 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentURL, nil)
		if err != nil {
			return "", err
		}
		applyLibBookHeaders(req, "")
		resp, err := client.Do(req)
		if err != nil {
			return "", upstreamRequestError(err, ErrLibBookUpstream, "libbook_timeout")
		}
		finalURL := resp.Request.URL.String()
		if token := extractLibBookCASToken(finalURL); token != "" {
			_ = resp.Body.Close()
			return token, nil
		}
		location := resp.Header.Get("Location")
		_ = resp.Body.Close()
		if token := extractLibBookCASToken(location); token != "" {
			return token, nil
		}
		if upstream.IsSSOURL(finalURL) && location == "" {
			return "", ErrLibBookAuth
		}
		if location == "" {
			return "", ErrLibBookAuth
		}
		currentURL = c.service.clients.UpstreamURL(resolveFeatureLocation(resp.Request.URL, location))
	}
	return "", ErrLibBookAuth
}

func (c *libBookClient) requestJSON(ctx context.Context, operation, path string, payload map[string]any, includeAuthorization, allowRetry bool) (map[string]any, error) {
	if includeAuthorization {
		if err := c.ensureLogin(ctx, false); err != nil {
			return nil, err
		}
	}
	client, err := c.service.clients.Get(c.username)
	if err != nil {
		return nil, err
	}
	rawPayload, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.service.clients.UpstreamURL(libBookBaseURL+"/v4/"+strings.TrimPrefix(path, "/")), bytes.NewReader(rawPayload))
	if err != nil {
		return nil, err
	}
	applyLibBookHeaders(req, c.token)
	if includeAuthorization {
		req.Header.Set("Authorization", "bearer"+c.token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Client.Do(req)
	if err != nil {
		return nil, upstreamRequestError(err, ErrLibBookUpstream, "libbook_timeout")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, libBookHTTPError(resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	raw := map[string]any{}
	if err := json.Unmarshal(body, &raw); err != nil {
		if isSessionExpired(resp, body) {
			return nil, ErrLibBookAuth
		}
		return nil, ErrLibBookUpstream
	}
	if isLibBookLoginExpired(raw) {
		c.token = ""
		if !allowRetry {
			return nil, ErrLibBookAuth
		}
		if err := c.ensureLogin(ctx, true); err != nil {
			return nil, err
		}
		return c.requestJSON(ctx, operation, path, payload, includeAuthorization, false)
	}
	if code, ok := libBookOptionalCode(raw); ok && code != 0 && code != 1 {
		return nil, libBookMessage(libBookMessageToError(libBookMessageOrDefault(raw, "图书馆接口请求失败")), libBookMessageOrDefault(raw, "图书馆接口请求失败"))
	}
	return raw, nil
}

func applyLibBookHeaders(req *http.Request, token string) {
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", libBookUserAgent)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", libBookBaseURL)
	req.Header.Set("Origin", libBookBaseURL)
}

func mapLibBookLibrary(raw any) dto.LibBookLibraryDto {
	item := libBookMap(raw)
	children := libBookSlice(item["children"])
	storeys := make([]dto.LibBookStoreyDto, 0, len(children))
	for _, child := range children {
		storeys = append(storeys, mapLibBookStorey(child))
	}
	return dto.LibBookLibraryDto{
		ID:       libBookString(item, "id"),
		Name:     libBookString(item, "name"),
		FreeNum:  libBookInt(item, "free_num"),
		TotalNum: libBookInt(item, "total_num"),
		Storeys:  storeys,
	}
}

func mapLibBookStorey(raw any) dto.LibBookStoreyDto {
	item := libBookMap(raw)
	return dto.LibBookStoreyDto{
		ID:       libBookString(item, "id"),
		Name:     libBookString(item, "name"),
		FreeNum:  libBookInt(item, "free_num"),
		TotalNum: libBookInt(item, "total_num"),
	}
}

func mapLibBookArea(raw any) dto.LibBookAreaDto {
	item := libBookMap(raw)
	return dto.LibBookAreaDto{
		ID:         libBookString(item, "id"),
		Name:       libBookString(item, "name"),
		AreaName:   libBookString(item, "area"),
		PremisesID: firstNonBlank(libBookString(item, "premises_id"), libBookString(item, "premisesId")),
		StoreyID:   firstNonBlank(libBookString(item, "storey_id"), libBookString(item, "storeyId")),
		FreeNum:    libBookInt(item, "free_num"),
		TotalNum:   libBookInt(item, "total_num"),
	}
}

func mapLibBookAreaDetail(areaID string, raw map[string]any) dto.LibBookAreaDetailDto {
	data := libBookMap(raw["data"])
	area := libBookMap(data["area"])
	date := libBookMap(data["date"])
	dateList := libBookSlice(date["list"])
	availableDates := []string{}
	timeSlots := []dto.LibBookTimeSlotDto{}
	for i, rawDate := range dateList {
		item := libBookMap(rawDate)
		if day := firstNonBlank(libBookString(item, "day"), libBookString(item, "date")); day != "" {
			availableDates = append(availableDates, day)
		}
		if i == 0 {
			for _, rawSlot := range libBookSlice(item["times"]) {
				slot := libBookMap(rawSlot)
				id := libBookString(slot, "id")
				if id == "" {
					continue
				}
				start := libBookString(slot, "start")
				end := libBookString(slot, "end")
				timeSlots = append(timeSlots, dto.LibBookTimeSlotDto{ID: id, Start: start, End: end, Label: start + "-" + end})
			}
		}
	}
	return dto.LibBookAreaDetailDto{
		ID:             firstNonBlank(libBookString(area, "id"), areaID),
		Name:           libBookString(area, "name"),
		AvailableDates: availableDates,
		TimeSlots:      timeSlots,
	}
}

func mapLibBookSeat(raw any) dto.LibBookSeatDto {
	item := libBookMap(raw)
	status := libBookString(item, "status")
	return dto.LibBookSeatDto{
		ID:          libBookString(item, "id"),
		Name:        libBookString(item, "name"),
		No:          libBookString(item, "no"),
		Status:      status,
		StatusName:  libBookString(item, "status_name"),
		IsAvailable: status == "1",
	}
}

func mapLibBookBooking(raw map[string]any) dto.LibBookBookingDto {
	return dto.LibBookBookingDto{
		ID:         libBookString(raw, "id"),
		NameMerge:  firstNonBlank(libBookString(raw, "nameMerge"), libBookString(raw, "name_merge")),
		AreaName:   firstNonBlank(libBookString(raw, "name"), libBookString(raw, "area_name")),
		SeatNo:     firstNonBlank(libBookString(raw, "no"), libBookString(raw, "seat_no")),
		Day:        firstNonBlank(libBookString(raw, "day"), libBookString(raw, "date")),
		BeginTime:  firstNonBlank(libBookString(raw, "beginTime"), libBookString(raw, "begin_time")),
		EndTime:    firstNonBlank(libBookString(raw, "endTime"), libBookString(raw, "end_time")),
		Status:     libBookString(raw, "status"),
		StatusName: libBookString(raw, "status_name"),
	}
}

func encryptLibBookReserveRequest(request dto.LibBookReserveRequest) string {
	body := map[string]any{
		"seat_id":    request.SeatID,
		"segment":    request.Segment,
		"day":        request.Day,
		"start_time": "",
		"end_time":   "",
	}
	raw, _ := json.Marshal(body)
	return encryptLibBookJSON(string(raw), request.Day)
}

func encryptLibBookJSON(plainText, day string) string {
	block, err := aes.NewCipher(libBookAESKey(day))
	if err != nil {
		return ""
	}
	plain := pkcs7Pad([]byte(plainText), aes.BlockSize)
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, []byte(libBookIVText)).CryptBlocks(encrypted, plain)
	return base64.StdEncoding.EncodeToString(encrypted)
}

func libBookAESKey(day string) []byte {
	digits := regexp.MustCompile(`\D`).ReplaceAllString(day, "")
	if len(digits) != 8 {
		return []byte("0000000000000000")
	}
	return []byte(digits + reverseString(digits))
}

func pkcs7Pad(raw []byte, blockSize int) []byte {
	padLength := blockSize - len(raw)%blockSize
	return append(raw, bytes.Repeat([]byte{byte(padLength)}, padLength)...)
}

func extractLibBookCASToken(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err == nil {
		if token := parsed.Query().Get("cas"); token != "" {
			return token
		}
	}
	match := regexp.MustCompile(`[?&#]cas=([^&]+)`).FindStringSubmatch(raw)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func isLibBookLoginExpired(raw map[string]any) bool {
	message := libBookMessageOrDefault(raw, "")
	return strings.Contains(message, "登录失效") || strings.Contains(message, "请重新登录") || strings.Contains(message, "未登录")
}

func libBookBusinessSuccess(raw map[string]any) bool {
	if code, ok := libBookOptionalCode(raw); ok && code != 0 && code != 1 {
		return false
	}
	message := libBookMessageOrDefault(raw, "")
	for _, marker := range []string{"失败", "不可", "已被", "不能取消", "无法取消", "已取消", "用户取消", "已结束", "已完成"} {
		if strings.Contains(message, marker) {
			return false
		}
	}
	return true
}

func libBookMessageToError(message string) error {
	for _, marker := range []string{"已取消", "用户取消", "已结束", "不能取消", "无法取消", "已完成", "不存在", "失效"} {
		if strings.Contains(message, marker) {
			return ErrLibBookNotFound
		}
	}
	for _, marker := range []string{"已被", "不可", "无可预约"} {
		if strings.Contains(message, marker) {
			return ErrLibBookSeatUnavailable
		}
	}
	return ErrLibBookUpstream
}

func libBookHTTPError(status int) error {
	switch status {
	case http.StatusGatewayTimeout:
		return timeoutError("libbook_timeout")
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrLibBookAuth
	case http.StatusNotFound:
		return ErrLibBookNotFound
	default:
		return ErrLibBookUpstream
	}
}

func libBookError(c *fiber.Ctx, err error) error {
	message := ""
	var withMessage libBookErrorWithMessage
	if errors.As(err, &withMessage) {
		message = withMessage.message
	}
	if code, ok := timeoutCode(err); ok {
		return httpx.Error(c, fiber.StatusGatewayTimeout, code, message)
	}
	switch {
	case errors.Is(err, ErrLibBookAuth):
		return httpx.Error(c, fiber.StatusBadGateway, "libbook_auth_failed")
	case errors.Is(err, ErrLibBookInvalidRequest):
		return httpx.Error(c, fiber.StatusBadRequest, "invalid_request", message)
	case errors.Is(err, ErrLibBookNotFound):
		return httpx.Error(c, fiber.StatusNotFound, "libbook_not_found", message)
	case errors.Is(err, ErrLibBookSeatUnavailable):
		return httpx.Error(c, fiber.StatusConflict, "libbook_seat_unavailable", message)
	default:
		return httpx.Error(c, fiber.StatusBadGateway, "libbook_error", message)
	}
}

func libBookMessage(err error, message string) error {
	return libBookErrorWithMessage{err: err, message: message}
}

func libBookMessageOrDefault(raw map[string]any, fallback string) string {
	return firstNonBlank(libBookString(raw, "message"), libBookString(raw, "msg"), fallback)
}

func libBookOptionalCode(raw map[string]any) (int, bool) {
	value, ok := raw["code"]
	if !ok || value == nil {
		return 0, false
	}
	return libBookIntAny(value, 0), true
}

func libBookString(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	return libBookStringAny(raw[key])
}

func libBookStringAny(value any) string {
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
	case json.Number:
		return typed.String()
	default:
		return strings.TrimSpace(strings.Trim(fmt.Sprint(typed), `"`))
	}
}

func libBookInt(raw map[string]any, key string) int {
	if raw == nil {
		return 0
	}
	return libBookIntAny(raw[key], 0)
}

func libBookIntAny(value any, fallback int) int {
	switch typed := value.(type) {
	case nil:
		return fallback
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed)
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func libBookMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func libBookSlice(value any) []any {
	if typed, ok := value.([]any); ok {
		return typed
	}
	return []any{}
}

func libBookMapValue(raw map[string]any, key string) any {
	if raw == nil {
		return nil
	}
	return raw[key]
}

func reverseString(value string) string {
	runes := []rune(value)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}

const (
	libBookBaseURL     = "https://booking.lib.buaa.edu.cn"
	libBookCASLoginURL = "https://sso.buaa.edu.cn/login?service=https%3A%2F%2Fbooking.lib.buaa.edu.cn%2Fv4%2Flogin%2Fcas"
	libBookIVText      = "ZZWBKJ_ZHIHUAWEI"
	libBookUserAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"
)
