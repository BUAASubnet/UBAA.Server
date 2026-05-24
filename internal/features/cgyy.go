package features

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BUAASubnet/UBAA.Server/internal/dto"
	"github.com/BUAASubnet/UBAA.Server/internal/httpx"
	"github.com/gofiber/fiber/v2"
)

var (
	ErrCgyyUnauthenticated         = errors.New("cgyy unauthenticated")
	ErrCgyyInvalidRequest          = errors.New("cgyy invalid request")
	ErrCgyyReservationInvalid      = errors.New("cgyy reservation invalid")
	ErrCgyyReservationTokenMissing = errors.New("cgyy reservation token missing")
	ErrCgyyCaptcha                 = errors.New("cgyy captcha error")
	ErrCgyyUpstream                = errors.New("cgyy upstream error")
)

type cgyyErrorWithCode struct {
	err     error
	message string
}

func (e cgyyErrorWithCode) Error() string { return e.message }
func (e cgyyErrorWithCode) Unwrap() error { return e.err }

func (s *Service) GetCgyyVenueSites(ctx context.Context, username string) ([]dto.CgyyVenueSiteDto, error) {
	raw, err := s.cgyyClient(username).VenueSites(ctx)
	if err != nil {
		return nil, err
	}
	items := cgyyVenueSiteArray(raw)
	result := make([]dto.CgyyVenueSiteDto, 0, len(items))
	for _, item := range items {
		result = append(result, mapCgyyVenueSite(item))
	}
	return result, nil
}

func (s *Service) GetCgyyPurposeTypes(ctx context.Context, username string) ([]dto.CgyyPurposeTypeDto, error) {
	raw, err := s.cgyyClient(username).PurposeTypesRaw(ctx)
	if err == nil {
		if parsed := parseCgyyPurposeTypes(raw); len(parsed) > 0 {
			return parsed, nil
		}
	}
	return fallbackCgyyPurposeTypes(), nil
}

func (s *Service) GetCgyyDayInfo(ctx context.Context, username string, venueSiteID int, date string) (dto.CgyyDayInfoResponse, error) {
	raw, err := s.cgyyClient(username).ReservationDayInfo(ctx, date, venueSiteID)
	if err != nil {
		return dto.CgyyDayInfoResponse{}, err
	}
	return mapCgyyDayInfo(venueSiteID, date, raw), nil
}

func (s *Service) SubmitCgyyReservation(ctx context.Context, username string, request dto.CgyyReservationSubmitRequest) (dto.CgyyReservationSubmitResponse, error) {
	if err := validateCgyyReservationRequest(request); err != nil {
		return dto.CgyyReservationSubmitResponse{}, err
	}
	client := s.cgyyClient(username)
	dayInfo, err := s.GetCgyyDayInfo(ctx, username, request.VenueSiteID, request.ReservationDate)
	if err != nil {
		return dto.CgyyReservationSubmitResponse{}, err
	}
	if dayInfo.ReservationToken == nil || strings.TrimSpace(*dayInfo.ReservationToken) == "" {
		return dto.CgyyReservationSubmitResponse{}, cgyyMessage(ErrCgyyReservationTokenMissing, "预约上下文 token 缺失，请刷新后重试")
	}
	selectedSpaceID := request.Selections[0].SpaceID
	var selectedSpace *dto.CgyySpaceAvailabilityDto
	for i := range dayInfo.Spaces {
		if dayInfo.Spaces[i].SpaceID == selectedSpaceID {
			selectedSpace = &dayInfo.Spaces[i]
			break
		}
	}
	if selectedSpace == nil {
		return dto.CgyyReservationSubmitResponse{}, cgyyMessage(ErrCgyyReservationInvalid, "所选房间不存在或已失效")
	}
	for _, selection := range request.Selections {
		if selection.SpaceID != selectedSpaceID {
			return dto.CgyyReservationSubmitResponse{}, cgyyMessage(ErrCgyyReservationInvalid, "同次预约只能选择同一房间的时段")
		}
		found := false
		for _, slot := range selectedSpace.Slots {
			if slot.TimeID == selection.TimeID {
				found = true
				if !slot.IsReservable {
					return dto.CgyyReservationSubmitResponse{}, cgyyMessage(ErrCgyyReservationInvalid, "所选时段已不可预约，请刷新后重试")
				}
			}
		}
		if !found {
			return dto.CgyyReservationSubmitResponse{}, cgyyMessage(ErrCgyyReservationInvalid, "所选时段不存在或已失效")
		}
	}
	rawOrder, _ := json.Marshal(request.Selections)
	reservationOrderJSON := string(rawOrder)
	if _, err := client.CreateReservationOrder(ctx, request.VenueSiteID, request.ReservationDate, request.ReservationDate, reservationOrderJSON, *dayInfo.ReservationToken); err != nil {
		return dto.CgyyReservationSubmitResponse{}, err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		challenge, err := client.Captcha(ctx)
		if err != nil {
			lastErr = err
			continue
		}
		solved, err := solveCgyyCaptcha(challenge)
		if err != nil {
			lastErr = err
			continue
		}
		if _, err := client.VerifyCaptcha(ctx, solved.PointJSON, challenge.Token); err != nil {
			lastErr = err
			continue
		}
		response, err := client.SubmitReservationOrder(ctx, request, reservationOrderJSON, solved.CaptchaVerification, *dayInfo.ReservationToken)
		if err != nil {
			lastErr = err
			continue
		}
		result := dto.CgyyReservationSubmitResponse{Success: true, Message: firstNonBlank(cgyyString(response, "message"), "预约成功")}
		if data := cgyyMap(response["data"]); data != nil {
			if orderInfo := cgyyMap(data["orderInfo"]); orderInfo != nil {
				order := mapCgyyOrder(orderInfo)
				result.Order = &order
			}
		}
		return result, nil
	}
	if lastErr != nil {
		return dto.CgyyReservationSubmitResponse{}, cgyyMessage(ErrCgyyCaptcha, lastErr.Error())
	}
	return dto.CgyyReservationSubmitResponse{}, cgyyMessage(ErrCgyyCaptcha, "验证码识别失败，请稍后重试")
}

func (s *Service) GetCgyyOrders(ctx context.Context, username string, page, size int) (dto.CgyyOrdersPageResponse, error) {
	raw, err := s.cgyyClient(username).MineOrders(ctx, page, size)
	if err != nil {
		return dto.CgyyOrdersPageResponse{}, err
	}
	contentRaw := cgyySlice(raw["content"])
	content := make([]dto.CgyyOrderDto, 0, len(contentRaw))
	for _, item := range contentRaw {
		if order := cgyyMap(item); order != nil {
			content = append(content, mapCgyyOrder(order))
		}
	}
	return dto.CgyyOrdersPageResponse{
		Content:       content,
		TotalElements: cgyyInt(raw, "totalElements"),
		TotalPages:    cgyyInt(raw, "totalPages"),
		Size:          cgyyIntDefault(raw, "size", size),
		Number:        cgyyIntDefault(raw, "number", page),
	}, nil
}

func (s *Service) GetCgyyOrderDetail(ctx context.Context, username string, orderID int) (dto.CgyyOrderDto, error) {
	raw, err := s.cgyyClient(username).OrderDetail(ctx, orderID)
	if err != nil {
		return dto.CgyyOrderDto{}, err
	}
	return mapCgyyOrder(raw), nil
}

func (s *Service) CancelCgyyOrder(ctx context.Context, username string, orderID int) (dto.CgyyReservationSubmitResponse, error) {
	raw, err := s.cgyyClient(username).CancelOrder(ctx, orderID)
	if err != nil {
		return dto.CgyyReservationSubmitResponse{}, err
	}
	return dto.CgyyReservationSubmitResponse{Success: true, Message: firstNonBlank(cgyyString(raw, "message"), "取消成功")}, nil
}

func (s *Service) GetCgyyLockCode(ctx context.Context, username string) (dto.CgyyLockCodeResponse, error) {
	raw, err := s.cgyyClient(username).LockCode(ctx)
	if err != nil {
		return dto.CgyyLockCodeResponse{}, err
	}
	return dto.CgyyLockCodeResponse{RawData: raw}, nil
}

func (s *Service) cgyyClient(username string) *cgyyClient {
	now := time.Now()
	if raw, ok := s.cgyyClients.Load(username); ok {
		cached := raw.(*cachedCgyyClient)
		cached.lastAccessAt = now
		return cached.client
	}
	client := &cgyyClient{username: username, service: s}
	cached := &cachedCgyyClient{client: client, lastAccessAt: now}
	actual, _ := s.cgyyClients.LoadOrStore(username, cached)
	return actual.(*cachedCgyyClient).client
}

type cgyyClient struct {
	username    string
	service     *Service
	accessToken string
}

type cgyyEnvelope struct {
	Data    any
	Message string
	Raw     map[string]any
}

func (c *cgyyClient) VenueSites(ctx context.Context) (any, error) {
	response, err := c.requestJSON(ctx, "GET", "/api/front/website/venues", map[string]any{"page": -1, "size": -1, "reservationRoleId": 3}, nil, true, true)
	if err != nil {
		return nil, err
	}
	return response.Data, nil
}

func (c *cgyyClient) PurposeTypesRaw(ctx context.Context) (any, error) {
	response, err := c.requestJSON(ctx, "GET", "/api/codes", nil, nil, true, true)
	if err != nil {
		return nil, err
	}
	return response.Data, nil
}

func (c *cgyyClient) ReservationDayInfo(ctx context.Context, searchDate string, venueSiteID int) (map[string]any, error) {
	response, err := c.requestJSON(ctx, "GET", "/api/reservation/day/info", map[string]any{"searchDate": searchDate, "venueSiteId": venueSiteID}, nil, true, true)
	if err != nil {
		return nil, err
	}
	data := cgyyMap(response.Data)
	if data == nil {
		return nil, cgyyMessage(ErrCgyyUpstream, "研讨室可用性响应为空")
	}
	return data, nil
}

func (c *cgyyClient) CreateReservationOrder(ctx context.Context, venueSiteID int, reservationDate, weekStartDate, reservationOrderJSON, token string) (*cgyyEnvelope, error) {
	return c.requestJSON(ctx, "POST", "/api/reservation/order/info", nil, map[string]any{
		"venueSiteId":          venueSiteID,
		"reservationDate":      reservationDate,
		"weekStartDate":        weekStartDate,
		"reservationOrderJson": reservationOrderJSON,
		"token":                token,
	}, true, true)
}

func (c *cgyyClient) Captcha(ctx context.Context) (cgyyCaptchaChallenge, error) {
	now := time.Now().UnixMilli()
	response, err := c.requestJSON(ctx, "GET", "/api/captcha/get", map[string]any{"captchaType": "blockPuzzle", "clientUid": "slider-" + strconv.FormatInt(now, 10), "ts": now}, nil, true, true)
	if err != nil {
		return cgyyCaptchaChallenge{}, err
	}
	data := cgyyMap(response.Data)
	if !cgyyBool(data, "success") {
		return cgyyCaptchaChallenge{}, cgyyMessage(ErrCgyyCaptcha, firstNonBlank(cgyyString(data, "repMsg"), "获取验证码失败"))
	}
	repData := cgyyMap(data["repData"])
	challenge := cgyyCaptchaChallenge{
		SecretKey:           cgyyString(repData, "secretKey"),
		Token:               cgyyString(repData, "token"),
		OriginalImageBase64: cgyyString(repData, "originalImageBase64"),
		JigsawImageBase64:   cgyyString(repData, "jigsawImageBase64"),
	}
	if challenge.SecretKey == "" || challenge.Token == "" || challenge.OriginalImageBase64 == "" || challenge.JigsawImageBase64 == "" {
		return cgyyCaptchaChallenge{}, cgyyMessage(ErrCgyyCaptcha, "验证码数据缺失")
	}
	return challenge, nil
}

func (c *cgyyClient) VerifyCaptcha(ctx context.Context, pointJSON, token string) (map[string]any, error) {
	response, err := c.requestJSON(ctx, "POST", "/api/captcha/check", nil, map[string]any{"pointJson": pointJSON, "token": token}, true, true)
	if err != nil {
		return nil, err
	}
	data := cgyyMap(response.Data)
	if !cgyyBool(data, "success") {
		return nil, cgyyMessage(ErrCgyyCaptcha, firstNonBlank(cgyyString(data, "repMsg"), "验证码校验失败"))
	}
	return data, nil
}

func (c *cgyyClient) SubmitReservationOrder(ctx context.Context, request dto.CgyyReservationSubmitRequest, reservationOrderJSON, captchaVerification, token string) (map[string]any, error) {
	response, err := c.requestJSON(ctx, "POST", "/api/reservation/order/submit", nil, map[string]any{
		"venueSiteId":                request.VenueSiteID,
		"reservationDate":            request.ReservationDate,
		"reservationOrderJson":       reservationOrderJSON,
		"weekStartDate":              request.ReservationDate,
		"phone":                      strings.TrimSpace(request.Phone),
		"theme":                      strings.TrimSpace(request.Theme),
		"purposeType":                request.PurposeType,
		"joinerNum":                  request.JoinerNum,
		"activityContent":            strings.TrimSpace(request.ActivityContent),
		"joiners":                    strings.TrimSpace(request.Joiners),
		"isPhilosophySocialSciences": boolIntValue(request.IsPhilosophySocialSciences),
		"isOffSchoolJoiner":          boolIntValue(request.IsOffSchoolJoiner),
		"captchaVerification":        captchaVerification,
		"token":                      token,
	}, true, true)
	if err != nil {
		return nil, err
	}
	return response.Raw, nil
}

func (c *cgyyClient) MineOrders(ctx context.Context, page, size int) (map[string]any, error) {
	response, err := c.requestJSON(ctx, "GET", "/api/orders/mine", map[string]any{"page": page, "size": size}, nil, true, true)
	if err != nil {
		return nil, err
	}
	return cgyyMap(response.Data), nil
}

func (c *cgyyClient) OrderDetail(ctx context.Context, orderID int) (map[string]any, error) {
	response, err := c.requestJSON(ctx, "GET", "/api/orders/"+strconv.Itoa(orderID), nil, nil, true, true)
	if err != nil {
		return nil, err
	}
	return cgyyMap(response.Data), nil
}

func (c *cgyyClient) CancelOrder(ctx context.Context, orderID int) (map[string]any, error) {
	response, err := c.requestJSON(ctx, "POST", "/api/orders/new/cancel/"+strconv.Itoa(orderID), nil, nil, true, true)
	if err != nil {
		return nil, err
	}
	return response.Raw, nil
}

func (c *cgyyClient) LockCode(ctx context.Context) (any, error) {
	response, err := c.requestJSON(ctx, "GET", "/api/orders/lock/code", nil, nil, true, true)
	if err != nil {
		return nil, err
	}
	return response.Data, nil
}

func (c *cgyyClient) requestJSON(ctx context.Context, method, path string, params, form map[string]any, includeAuthorization, allowRetry bool) (*cgyyEnvelope, error) {
	if includeAuthorization {
		if err := c.ensureBusinessLogin(ctx); err != nil {
			return nil, err
		}
	}
	sessionClient, err := c.service.clients.Get(c.username)
	if err != nil {
		return nil, err
	}
	timestamp := time.Now().UnixMilli()
	pathOnly := normalizeCgyyPath(path)
	requestParams := params
	if method == http.MethodGet {
		requestParams = addCgyyNoCacheIfMissing(params, timestamp)
	}
	signSource := requestParams
	if method != http.MethodGet {
		signSource = form
	}
	sign := signCgyy(pathOnly, signSource, timestamp)
	target := c.service.clients.UpstreamURL(cgyyBaseURL + strings.TrimPrefix(pathOnly, "/"))
	var body io.Reader
	if method != http.MethodGet {
		values := url.Values{}
		for key, value := range form {
			if value != nil {
				values.Set(key, cgyyAnyString(value))
			}
		}
		body = strings.NewReader(values.Encode())
	} else if len(requestParams) > 0 {
		parsed, _ := url.Parse(target)
		query := parsed.Query()
		for key, value := range requestParams {
			if value != nil {
				query.Set(key, cgyyAnyString(value))
			}
		}
		parsed.RawQuery = query.Encode()
		target = parsed.String()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", "https://cgyy.buaa.edu.cn/venue-zhjs/mobileReservation")
	req.Header.Set("app-key", cgyyAppKey)
	req.Header.Set("timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("sign", sign)
	if includeAuthorization {
		req.Header.Set("cgAuthorization", c.accessToken)
	}
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := sessionClient.Client.Do(req)
	if err != nil {
		return nil, upstreamRequestError(err, ErrCgyyUpstream, "cgyy_timeout")
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	if isCgyyLoginRedirect(resp, bodyBytes) {
		c.accessToken = ""
		if !allowRetry {
			return nil, ErrCgyyUnauthenticated
		}
		if err := c.ensureBusinessLogin(ctx); err != nil {
			return nil, err
		}
		return c.requestJSON(ctx, method, path, params, form, includeAuthorization, false)
	}
	raw := map[string]any{}
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		return nil, ErrCgyyUpstream
	}
	if cgyyInt(raw, "code") != 200 {
		return nil, cgyyMessage(ErrCgyyUpstream, firstNonBlank(cgyyString(raw, "message"), "研讨室系统请求失败"))
	}
	return &cgyyEnvelope{
		Data:    raw["data"],
		Message: firstNonBlank(cgyyString(raw, "message"), "OK"),
		Raw:     raw,
	}, nil
}

func (c *cgyyClient) ensureBusinessLogin(ctx context.Context) error {
	if strings.TrimSpace(c.accessToken) != "" {
		return nil
	}
	sessionClient, err := c.service.clients.Get(c.username)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.service.clients.UpstreamURL(cgyyBaseURL+"sso/manageLogin"), nil)
	if err != nil {
		return err
	}
	resp, err := sessionClient.Client.Do(req)
	if err != nil {
		return upstreamRequestError(err, ErrCgyyUpstream, "cgyy_timeout")
	}
	_ = resp.Body.Close()
	ssoToken := c.findSSOToken(sessionClient.Client.Jar)
	if ssoToken == "" {
		return cgyyMessage(ErrCgyyUnauthenticated, "未获取到研讨室 SSO Token")
	}
	response, err := c.requestJSONWithExtraHeaders(ctx, http.MethodPost, "/api/login", nil, nil, map[string]string{"Sso-Token": ssoToken}, false)
	if err != nil {
		return err
	}
	token := cgyyString(cgyyMap(cgyyMap(response.Data)["token"]), "access_token")
	if token == "" {
		return cgyyMessage(ErrCgyyUnauthenticated, "研讨室登录成功但未返回 access_token")
	}
	c.accessToken = token
	return nil
}

func (c *cgyyClient) requestJSONWithExtraHeaders(ctx context.Context, method, path string, params, form map[string]any, extraHeaders map[string]string, includeAuthorization bool) (*cgyyEnvelope, error) {
	sessionClient, err := c.service.clients.Get(c.username)
	if err != nil {
		return nil, err
	}
	timestamp := time.Now().UnixMilli()
	pathOnly := normalizeCgyyPath(path)
	signSource := params
	if method != http.MethodGet {
		signSource = form
	}
	sign := signCgyy(pathOnly, signSource, timestamp)
	target := c.service.clients.UpstreamURL(cgyyBaseURL + strings.TrimPrefix(pathOnly, "/"))
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", "https://cgyy.buaa.edu.cn/venue-zhjs/mobileReservation")
	req.Header.Set("app-key", cgyyAppKey)
	req.Header.Set("timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("sign", sign)
	for key, value := range extraHeaders {
		req.Header.Set(key, value)
	}
	resp, err := sessionClient.Client.Do(req)
	if err != nil {
		return nil, upstreamRequestError(err, ErrCgyyUpstream, "cgyy_timeout")
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(resp.Body)
	raw := map[string]any{}
	if err := json.Unmarshal(rawBody, &raw); err != nil {
		return nil, ErrCgyyUpstream
	}
	if cgyyInt(raw, "code") != 200 {
		return nil, cgyyMessage(ErrCgyyUpstream, firstNonBlank(cgyyString(raw, "message"), "研讨室系统请求失败"))
	}
	return &cgyyEnvelope{Data: raw["data"], Message: firstNonBlank(cgyyString(raw, "message"), "OK"), Raw: raw}, nil
}

func (c *cgyyClient) findSSOToken(jar http.CookieJar) string {
	if jar == nil {
		return ""
	}
	parsed, _ := url.Parse(cgyyBaseURL)
	for _, cookie := range jar.Cookies(parsed) {
		if cookie.Name == cgyySSOCookieName && strings.TrimSpace(cookie.Value) != "" {
			return cookie.Value
		}
	}
	return ""
}

type cgyyCaptchaChallenge struct {
	SecretKey           string
	Token               string
	OriginalImageBase64 string
	JigsawImageBase64   string
}

type cgyySolvedCaptcha struct {
	MoveDistance        int
	PointJSONData       string
	PointJSON           string
	CaptchaVerification string
}

func solveCgyyCaptcha(challenge cgyyCaptchaChallenge) (cgyySolvedCaptcha, error) {
	background, err := decodeCgyyBase64Image(challenge.OriginalImageBase64)
	if err != nil {
		return cgyySolvedCaptcha{}, err
	}
	piece, err := decodeCgyyBase64Image(challenge.JigsawImageBase64)
	if err != nil {
		return cgyySolvedCaptcha{}, err
	}
	moveDistance, err := solveCgyyOffset(background, piece)
	if err != nil {
		return cgyySolvedCaptcha{}, err
	}
	pointJSONData := `{"x":` + strconv.Itoa(moveDistance) + `,"y":5}`
	pointJSON, err := encryptCgyyCaptcha(pointJSONData, challenge.SecretKey)
	if err != nil {
		return cgyySolvedCaptcha{}, err
	}
	captchaVerification, err := encryptCgyyCaptcha(challenge.Token+"---"+pointJSONData, challenge.SecretKey)
	if err != nil {
		return cgyySolvedCaptcha{}, err
	}
	return cgyySolvedCaptcha{MoveDistance: moveDistance, PointJSONData: pointJSONData, PointJSON: pointJSON, CaptchaVerification: captchaVerification}, nil
}

func signCgyy(path string, params map[string]any, timestamp int64) string {
	normalizedPath := normalizeCgyyPath(path)
	cleaned := cleanCgyySignParams(params)
	keys := make([]string, 0, len(cleaned))
	for key := range cleaned {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	builder.WriteString(cgyySignPrefix)
	builder.WriteString(normalizedPath)
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteString(cgyyAnyString(cleaned[key]))
	}
	builder.WriteString(strconv.FormatInt(timestamp, 10))
	builder.WriteByte(' ')
	builder.WriteString(cgyySignPrefix)
	digest := md5.Sum([]byte(builder.String()))
	return hex.EncodeToString(digest[:])
}

func cleanCgyySignParams(params map[string]any) map[string]any {
	cleaned := map[string]any{}
	for key, value := range params {
		if _, skip := cgyyRemovedSignKeys[key]; skip || !cgyyPrimitiveForSign(value) {
			continue
		}
		cleaned[key] = value
	}
	return cleaned
}

func addCgyyNoCacheIfMissing(params map[string]any, timestamp int64) map[string]any {
	result := map[string]any{}
	for key, value := range params {
		result[key] = value
	}
	if _, ok := result["nocache"]; !ok {
		result["nocache"] = timestamp
	}
	return result
}

func mapCgyyVenueSite(raw map[string]any) dto.CgyyVenueSiteDto {
	return dto.CgyyVenueSiteDto{
		ID:                    cgyyInt(raw, "id"),
		SiteName:              cgyyString(raw, "siteName"),
		VenueName:             cgyyString(raw, "venueName"),
		CampusName:            cgyyString(raw, "campusName"),
		SeatCount:             cgyyIntPtr(raw, "seatCount"),
		ReservationSpaceCount: cgyyIntPtr(raw, "reservationSpaceCount"),
		SiteTelephone:         stringPtrNonBlank(cgyyString(raw, "siteTelephone")),
		OpenStartDate:         stringPtrNonBlank(cgyyString(raw, "openStartDate")),
		OpenEndDate:           stringPtrNonBlank(cgyyString(raw, "openEndDate")),
	}
}

func cgyyVenueSiteArray(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []any:
		result := []map[string]any{}
		for _, item := range typed {
			if site := cgyyMap(item); site != nil {
				result = append(result, site)
			}
		}
		return result
	case map[string]any:
		result := []map[string]any{}
		for _, venueRaw := range cgyySlice(typed["content"]) {
			venue := cgyyMap(venueRaw)
			venueID := cgyyInt(venue, "id")
			venueName := cgyyString(venue, "venueName")
			campusName := cgyyString(venue, "campusName")
			for _, siteRaw := range cgyySlice(venue["siteList"]) {
				site := cgyyMap(siteRaw)
				if site == nil {
					continue
				}
				flattened := map[string]any{}
				for key, value := range site {
					flattened[key] = value
				}
				flattened["venueId"] = venueID
				flattened["venueName"] = venueName
				flattened["campusName"] = campusName
				result = append(result, flattened)
			}
		}
		return result
	default:
		return nil
	}
}

func mapCgyyDayInfo(venueSiteID int, reservationDate string, raw map[string]any) dto.CgyyDayInfoResponse {
	timeSlots := []dto.CgyyTimeSlotDto{}
	timeSlotIDs := map[int]bool{}
	for _, element := range cgyySlice(raw["spaceTimeInfo"]) {
		slot := cgyyMap(element)
		id := cgyyInt(slot, "id")
		begin := cgyyString(slot, "beginTime")
		end := cgyyString(slot, "endTime")
		if id == 0 || begin == "" || end == "" {
			continue
		}
		timeSlotIDs[id] = true
		timeSlots = append(timeSlots, dto.CgyyTimeSlotDto{ID: id, BeginTime: begin, EndTime: end, Label: begin + "-" + end})
	}
	reservationDateSpaceInfo := cgyyMap(raw["reservationDateSpaceInfo"])
	dateKey := reservationDate
	if _, ok := reservationDateSpaceInfo[dateKey]; !ok {
		for key := range reservationDateSpaceInfo {
			dateKey = key
			break
		}
	}
	spaces := []dto.CgyySpaceAvailabilityDto{}
	for _, element := range cgyySlice(reservationDateSpaceInfo[dateKey]) {
		space := cgyyMap(element)
		spaceID := cgyyInt(space, "id")
		slots := []dto.CgyySlotStatusDto{}
		for _, slot := range timeSlots {
			rawSlot := cgyyMap(space[strconv.Itoa(slot.ID)])
			if rawSlot == nil {
				continue
			}
			status := mapCgyySlot(slot.ID, rawSlot)
			if timeSlotIDs[status.TimeID] {
				slots = append(slots, status)
			}
		}
		sort.SliceStable(slots, func(i, j int) bool { return slots[i].TimeID < slots[j].TimeID })
		spaces = append(spaces, dto.CgyySpaceAvailabilityDto{
			SpaceID:           spaceID,
			SpaceName:         cgyyString(space, "spaceName"),
			VenueSiteID:       cgyyIntDefault(space, "venueSiteId", venueSiteID),
			VenueSpaceGroupID: cgyyIntPtr(space, "venueSpaceGroupId"),
			Slots:             slots,
		})
	}
	sort.SliceStable(spaces, func(i, j int) bool { return spaces[i].SpaceName < spaces[j].SpaceName })
	return dto.CgyyDayInfoResponse{
		VenueSiteID:         venueSiteID,
		ReservationDate:     dateKey,
		AvailableDates:      cgyyStringList(firstCgyyNonNil(raw["ableReservationDateList"], raw["reservationDateList"])),
		TimeSlots:           timeSlots,
		Spaces:              spaces,
		ReservationToken:    stringPtrNonBlank(cgyyString(raw, "token")),
		ReservationTotalNum: cgyyIntPtr(raw, "reservationTotalNum"),
	}
}

func mapCgyySlot(timeID int, raw map[string]any) dto.CgyySlotStatusDto {
	reservationStatus := cgyyInt(raw, "reservationStatus")
	tradeNo := stringPtrNonBlank(cgyyString(raw, "tradeNo"))
	orderID := cgyyIntPtr(raw, "orderId")
	takeUp := cgyyBoolPtr(raw, "takeUp")
	isReservable := reservationStatus == 1 && tradeNo == nil && orderID == nil && (takeUp == nil || !*takeUp)
	return dto.CgyySlotStatusDto{
		TimeID:            timeID,
		ReservationStatus: reservationStatus,
		IsReservable:      isReservable,
		StartDate:         stringPtrNonBlank(cgyyString(raw, "startDate")),
		EndDate:           stringPtrNonBlank(cgyyString(raw, "endDate")),
		TradeNo:           tradeNo,
		OrderID:           orderID,
		UseNum:            cgyyIntPtr(raw, "useNum"),
		AlreadyNum:        cgyyIntPtr(raw, "alreadyNum"),
		TakeUp:            takeUp,
		TakeUpExplain:     stringPtrNonBlank(cgyyString(raw, "takeUpExplain")),
	}
}

func mapCgyyOrder(raw map[string]any) dto.CgyyOrderDto {
	purposeType := cgyyIntPtr(raw, "purposeType")
	var purposeTypeName *string
	if purposeType != nil {
		purposeTypeName = stringPtrNonBlank(cgyyPurposeTypeName(*purposeType))
	}
	return dto.CgyyOrderDto{
		ID:                    cgyyInt(raw, "id"),
		TradeNo:               stringPtrNonBlank(cgyyString(raw, "tradeNo")),
		VenueSiteID:           cgyyIntPtr(raw, "venueSiteId"),
		ReservationDate:       stringPtrNonBlank(cgyyString(raw, "reservationDate")),
		ReservationDateDetail: stringPtrNonBlank(cgyyString(raw, "reservationDateDetail")),
		VenueSpaceName:        stringPtrNonBlank(cgyyString(raw, "venueSpaceName")),
		CampusName:            stringPtrNonBlank(cgyyString(raw, "campusName")),
		VenueName:             stringPtrNonBlank(cgyyString(raw, "venueName")),
		SiteName:              stringPtrNonBlank(cgyyString(raw, "siteName")),
		ReservationStartDate:  stringPtrNonBlank(cgyyString(raw, "reservationStartDate")),
		ReservationEndDate:    stringPtrNonBlank(cgyyString(raw, "reservationEndDate")),
		Phone:                 stringPtrNonBlank(cgyyString(raw, "phone")),
		OrderStatus:           cgyyIntPtr(raw, "orderStatus"),
		PayStatus:             cgyyIntPtr(raw, "payStatus"),
		CheckStatus:           cgyyIntPtr(raw, "checkStatus"),
		Theme:                 stringPtrNonBlank(cgyyString(raw, "theme")),
		PurposeType:           purposeType,
		PurposeTypeName:       purposeTypeName,
		JoinerNum:             cgyyIntPtr(raw, "joinerNum"),
		ActivityContent:       stringPtrNonBlank(cgyyString(raw, "activityContent")),
		Joiners:               stringPtrNonBlank(cgyyString(raw, "joiners")),
		CheckContent:          stringPtrNonBlank(cgyyString(raw, "checkContent")),
		HandleReason:          stringPtrNonBlank(cgyyString(raw, "handleReason")),
		Remark:                stringPtrNonBlank(cgyyString(raw, "remark")),
	}
}

func parseCgyyPurposeTypes(raw any) []dto.CgyyPurposeTypeDto {
	results := map[int]string{}
	var traverse func(any)
	traverse = func(value any) {
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				traverse(item)
			}
		case map[string]any:
			name := cgyyString(typed, "name")
			key := cgyyIntAny(firstCgyyNonNil(typed["key"], typed["value"], typed["id"]), 0)
			if key != 0 && strings.Contains(name, "类") {
				if _, exists := results[key]; !exists {
					results[key] = name
				}
			}
			for _, item := range typed {
				traverse(item)
			}
		}
	}
	traverse(raw)
	keys := make([]int, 0, len(results))
	for key := range results {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	types := make([]dto.CgyyPurposeTypeDto, 0, len(keys))
	for _, key := range keys {
		types = append(types, dto.CgyyPurposeTypeDto{Key: key, Name: results[key]})
	}
	return types
}

func fallbackCgyyPurposeTypes() []dto.CgyyPurposeTypeDto {
	return []dto.CgyyPurposeTypeDto{
		{Key: 1, Name: "导学活动类"},
		{Key: 2, Name: "学业支持类（串讲、答疑、学习小组讨论等）"},
		{Key: 3, Name: "学术研讨类（竞赛、答辩、展示等小组讨论）"},
		{Key: 4, Name: "党建活动类"},
		{Key: 5, Name: "工作会议类（单位工作例会、学生组织工作会议等）"},
		{Key: 6, Name: "团队建设类（班级、社团、学生会等学生组织团建）"},
		{Key: 7, Name: "培训面试类（梦拓、学生组织培训及面试等）"},
		{Key: 8, Name: "博雅课程类"},
		{Key: 9, Name: "讲座、沙龙研讨类"},
		{Key: 10, Name: "其他特色活动类"},
	}
}

func cgyyPurposeTypeName(key int) string {
	for _, item := range fallbackCgyyPurposeTypes() {
		if item.Key == key {
			return item.Name
		}
	}
	return ""
}

func validateCgyyReservationRequest(request dto.CgyyReservationSubmitRequest) error {
	if len(request.Selections) == 0 {
		return cgyyMessage(ErrCgyyInvalidRequest, "请至少选择一个时段")
	}
	if strings.TrimSpace(request.Phone) == "" {
		return cgyyMessage(ErrCgyyInvalidRequest, "请填写联系电话")
	}
	if strings.TrimSpace(request.Theme) == "" {
		return cgyyMessage(ErrCgyyInvalidRequest, "请填写活动主题")
	}
	if request.JoinerNum <= 0 {
		return cgyyMessage(ErrCgyyInvalidRequest, "参与人数必须大于 0")
	}
	if strings.TrimSpace(request.ActivityContent) == "" {
		return cgyyMessage(ErrCgyyInvalidRequest, "请填写活动内容")
	}
	if strings.TrimSpace(request.Joiners) == "" {
		return cgyyMessage(ErrCgyyInvalidRequest, "请填写参与人说明")
	}
	return nil
}

func cgyyError(c *fiber.Ctx, err error) error {
	if code, ok := timeoutCode(err); ok {
		return httpx.Error(c, fiber.StatusGatewayTimeout, code)
	}
	switch {
	case errors.Is(err, ErrCgyyInvalidRequest):
		return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
	case errors.Is(err, ErrCgyyReservationInvalid):
		return httpx.Error(c, fiber.StatusBadRequest, "reservation_invalid")
	case errors.Is(err, ErrCgyyReservationTokenMissing):
		return httpx.Error(c, fiber.StatusBadRequest, "reservation_token_missing")
	case errors.Is(err, ErrCgyyUnauthenticated):
		return httpx.Error(c, fiber.StatusUnauthorized, "unauthenticated")
	case errors.Is(err, ErrCgyyCaptcha):
		return httpx.Error(c, fiber.StatusBadGateway, "captcha_error")
	default:
		return httpx.Error(c, fiber.StatusBadGateway, "cgyy_error")
	}
}

func cgyyMessage(err error, message string) error {
	return cgyyErrorWithCode{err: err, message: message}
}

func isCgyyLoginRedirect(resp *http.Response, body []byte) bool {
	if resp.StatusCode == http.StatusUnauthorized {
		return true
	}
	finalURL := resp.Request.URL.String()
	if strings.Contains(finalURL, "sso.buaa.edu.cn/login") {
		return true
	}
	text := string(body)
	return strings.Contains(text, `name="execution"`) && strings.Contains(text, "username_password")
}

func normalizeCgyyPath(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func cgyyPrimitiveForSign(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return typed != ""
	case []any, []string, map[string]any:
		return false
	default:
		return true
	}
}

func decodeCgyyBase64Image(raw string) (image.Image, error) {
	payload := raw
	if index := strings.Index(payload, "base64,"); index >= 0 {
		payload = payload[index+len("base64,"):]
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, cgyyMessage(ErrCgyyCaptcha, "验证码图片解码失败")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, cgyyMessage(ErrCgyyCaptcha, "验证码图片解码失败")
	}
	return img, nil
}

func solveCgyyOffset(background, piece image.Image) (int, error) {
	bgGray := cgyyToGray(background)
	pieceGray := cgyyToGray(piece)
	mask := cgyyBuildMask(piece)
	bounds, ok := cgyyFindBoundingBox(mask)
	if !ok {
		return 0, cgyyMessage(ErrCgyyCaptcha, "无法识别滑块图片")
	}
	croppedPiece := cgyyCropInt(pieceGray, bounds)
	croppedMask := cgyyCropBool(mask, bounds)
	bgEdges := cgyyEdgeDetect(bgGray)
	pieceEdges := cgyyEdgeDetect(croppedPiece)
	bestScore := math.Inf(-1)
	bestX := 0
	yMax := max(len(bgEdges)-len(pieceEdges), 0)
	xMax := 0
	if len(bgEdges) > 0 && len(pieceEdges) > 0 {
		xMax = max(len(bgEdges[0])-len(pieceEdges[0]), 0)
	}
	for y := 0; y <= yMax; y++ {
		for x := 0; x <= xMax; x++ {
			score := 0.0
			edgePixels := 0
			maskPixels := 0
			for py := range pieceEdges {
				for px := range pieceEdges[py] {
					if !croppedMask[py][px] {
						continue
					}
					maskPixels++
					bgValue := bgEdges[y+py][x+px]
					pieceValue := pieceEdges[py][px]
					if pieceValue > 0 {
						edgePixels++
						if bgValue > 0 {
							score += 3.0
						} else {
							score -= 1.5
						}
					} else if bgValue == 0 {
						score += 0.15
					}
				}
			}
			if maskPixels == 0 || edgePixels == 0 {
				continue
			}
			score /= float64(edgePixels)
			score += float64(maskPixels) * 0.0001
			if score > bestScore {
				bestScore = score
				bestX = x
			}
		}
	}
	return bestX, nil
}

func encryptCgyyCaptcha(plainText, secretKey string) (string, error) {
	key := []byte(secretKey)
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return "", cgyyMessage(ErrCgyyCaptcha, "无效的验证码密钥长度")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", cgyyMessage(ErrCgyyCaptcha, "验证码加密失败")
	}
	padded := pkcs7Pad([]byte(plainText), aes.BlockSize)
	encrypted := make([]byte, len(padded))
	for start := 0; start < len(padded); start += aes.BlockSize {
		block.Encrypt(encrypted[start:start+aes.BlockSize], padded[start:start+aes.BlockSize])
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

func cgyyToGray(img image.Image) [][]int {
	bounds := img.Bounds()
	result := make([][]int, bounds.Dy())
	for y := 0; y < bounds.Dy(); y++ {
		result[y] = make([]int, bounds.Dx())
		for x := 0; x < bounds.Dx(); x++ {
			r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			result[y][x] = (int(r>>8)*30 + int(g>>8)*59 + int(b>>8)*11) / 100
		}
	}
	return result
}

func cgyyBuildMask(img image.Image) [][]bool {
	bounds := img.Bounds()
	result := make([][]bool, bounds.Dy())
	for y := 0; y < bounds.Dy(); y++ {
		result[y] = make([]bool, bounds.Dx())
		for x := 0; x < bounds.Dx(); x++ {
			c := color.NRGBAModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			if c.A > 10 {
				result[y][x] = true
				continue
			}
			luminance := (int(c.R)*30 + int(c.G)*59 + int(c.B)*11) / 100
			result[y][x] = luminance < 250
		}
	}
	return result
}

func cgyyEdgeDetect(gray [][]int) [][]int {
	height := len(gray)
	width := 0
	if height > 0 {
		width = len(gray[0])
	}
	result := make([][]int, height)
	for y := 0; y < height; y++ {
		result[y] = make([]int, width)
		for x := 0; x < width; x++ {
			center := gray[y][x]
			right := gray[y][min(x+1, width-1)]
			down := gray[min(y+1, height-1)][x]
			edge := abs(center-right) + abs(center-down)
			if edge > 35 {
				result[y][x] = 255
			}
		}
	}
	return result
}

func cgyyFindBoundingBox(mask [][]bool) ([4]int, bool) {
	minX, minY := math.MaxInt, math.MaxInt
	maxX, maxY := math.MinInt, math.MinInt
	for y := range mask {
		for x := range mask[y] {
			if !mask[y][x] {
				continue
			}
			minX = min(minX, x)
			minY = min(minY, y)
			maxX = max(maxX, x)
			maxY = max(maxY, y)
		}
	}
	if minX == math.MaxInt {
		return [4]int{}, false
	}
	return [4]int{minX, minY, maxX, maxY}, true
}

func cgyyCropInt(source [][]int, bounds [4]int) [][]int {
	width := bounds[2] - bounds[0] + 1
	height := bounds[3] - bounds[1] + 1
	result := make([][]int, height)
	for y := 0; y < height; y++ {
		result[y] = make([]int, width)
		for x := 0; x < width; x++ {
			result[y][x] = source[bounds[1]+y][bounds[0]+x]
		}
	}
	return result
}

func cgyyCropBool(source [][]bool, bounds [4]int) [][]bool {
	width := bounds[2] - bounds[0] + 1
	height := bounds[3] - bounds[1] + 1
	result := make([][]bool, height)
	for y := 0; y < height; y++ {
		result[y] = make([]bool, width)
		for x := 0; x < width; x++ {
			result[y][x] = source[bounds[1]+y][bounds[0]+x]
		}
	}
	return result
}

func cgyyString(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	return cgyyAnyString(raw[key])
}

func cgyyAnyString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case json.Number:
		return typed.String()
	default:
		return strings.TrimSpace(strings.Trim(fmt.Sprint(typed), `"`))
	}
}

func cgyyInt(raw map[string]any, key string) int {
	return cgyyIntAny(raw[key], 0)
}

func cgyyIntDefault(raw map[string]any, key string, fallback int) int {
	return cgyyIntAny(raw[key], fallback)
}

func cgyyIntAny(value any, fallback int) int {
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
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return fallback
}

func cgyyIntPtr(raw map[string]any, key string) *int {
	if raw == nil || raw[key] == nil {
		return nil
	}
	value := cgyyIntAny(raw[key], 0)
	return &value
}

func cgyyBool(raw map[string]any, key string) bool {
	if raw == nil {
		return false
	}
	switch typed := raw[key].(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	default:
		return false
	}
}

func cgyyBoolPtr(raw map[string]any, key string) *bool {
	if raw == nil || raw[key] == nil {
		return nil
	}
	value := cgyyBool(raw, key)
	return &value
}

func cgyyMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func cgyySlice(value any) []any {
	if typed, ok := value.([]any); ok {
		return typed
	}
	return nil
}

func cgyyStringList(value any) []string {
	result := []string{}
	for _, item := range cgyySlice(value) {
		if text := cgyyAnyString(item); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func firstCgyyNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func boolIntValue(value bool) int {
	if value {
		return 1
	}
	return 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

const (
	cgyyBaseURL       = "https://cgyy.buaa.edu.cn/venue-zhjs-server/"
	cgyySSOCookieName = "sso_buaa_zhjs_token"
	cgyySignPrefix    = "c640ca392cd45fb3a55b00a63a86c618"
	cgyyAppKey        = "8fceb735082b5a529312040b58ea780b"
)

var cgyyRemovedSignKeys = map[string]struct{}{
	"gmtCreate":   {},
	"gmtModified": {},
	"creator":     {},
	"modifier":    {},
	"id":          {},
	"_index":      {},
	"_rowKey":     {},
}
