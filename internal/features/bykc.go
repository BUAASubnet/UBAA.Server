package features

import (
	"context"
	"crypto/aes"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	mrand "math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BUAASubnet/UBAA.Server/internal/dto"
	"github.com/BUAASubnet/UBAA.Server/internal/httpx"
	"github.com/gofiber/fiber/v2"
)

var (
	ErrBykcInvalidRequest = errors.New("bykc invalid request")
	ErrBykcUpstream       = errors.New("bykc upstream error")
	ErrBykcBusiness       = errors.New("bykc business error")
)

type bykcErrorWithMessage struct {
	err     error
	message string
}

func (e bykcErrorWithMessage) Error() string { return e.message }
func (e bykcErrorWithMessage) Unwrap() error { return e.err }

func (s *Service) GetBykcUserProfile(ctx context.Context, username string) (dto.BykcUserProfileDto, error) {
	var raw map[string]any
	if err := s.bykcClient(username).callAPI(ctx, "getUserProfile", `{}`, &raw); err != nil {
		return dto.BykcUserProfileDto{}, err
	}
	return dto.BykcUserProfileDto{
		ID:          int64(bykcInt(raw, "id")),
		EmployeeID:  bykcString(raw, "employeeId"),
		RealName:    bykcString(raw, "realName"),
		StudentNo:   stringPtrNonBlank(bykcString(raw, "studentNo")),
		StudentType: stringPtrNonBlank(bykcString(raw, "studentType")),
		ClassCode:   stringPtrNonBlank(bykcString(raw, "classCode")),
		CollegeName: stringPtrNonBlank(bykcString(bykcMap(raw["college"]), "collegeName")),
		TermName:    stringPtrNonBlank(bykcString(bykcMap(raw["term"]), "termName")),
	}, nil
}

func (s *Service) GetBykcCourses(ctx context.Context, username string, page, size int, includeAll bool) (dto.BykcCoursesResponse, error) {
	raw, err := s.bykcClient(username).coursePage(ctx, page, size)
	if err != nil {
		return dto.BykcCoursesResponse{}, err
	}
	content := bykcSlice(raw["content"])
	courses := make([]dto.BykcCourseDto, 0, len(content))
	now := time.Now()
	for _, item := range content {
		course := bykcMap(item)
		status := bykcCourseStatus(course, now)
		if !includeAll && (status == bykcStatusExpired || status == bykcStatusEnded) {
			continue
		}
		courses = append(courses, mapBykcCourse(course, status))
	}
	return dto.BykcCoursesResponse{
		Courses:     courses,
		Total:       bykcInt(raw, "totalElements"),
		TotalPages:  bykcInt(raw, "totalPages"),
		CurrentPage: page,
		PageSize:    size,
	}, nil
}

func (s *Service) SelectBykcCourse(ctx context.Context, username string, courseID int64) (string, error) {
	request := fmt.Sprintf(`{"courseId":%d}`, courseID)
	if err := s.bykcClient(username).callAPI(ctx, "choseCourse", request, nil); err != nil {
		return "", err
	}
	return "选课成功", nil
}

func (s *Service) DeselectBykcCourse(ctx context.Context, username string, courseID int64) (string, error) {
	request := fmt.Sprintf(`{"id":%d}`, courseID)
	if err := s.bykcClient(username).callAPI(ctx, "delChosenCourse", request, nil); err != nil {
		return "", err
	}
	return "退选成功", nil
}

func (s *Service) GetBykcChosenCourses(ctx context.Context, username string) ([]dto.BykcChosenCourseDto, error) {
	client := s.bykcClient(username)
	config, err := client.allConfig(ctx)
	if err != nil {
		return nil, err
	}
	semester := resolveBykcCurrentSemester(config, time.Now())
	if semester == nil {
		return nil, bykcMessage(ErrBykcBusiness, "无法获取当前学期信息")
	}
	startDate := bykcString(semester, "semesterStartDate")
	endDate := bykcString(semester, "semesterEndDate")
	if startDate == "" || endDate == "" {
		return nil, bykcMessage(ErrBykcBusiness, "无法获取当前学期信息")
	}
	chosen, err := client.chosenCourses(ctx, startDate, endDate)
	if err != nil {
		return nil, err
	}
	result := make([]dto.BykcChosenCourseDto, 0, len(chosen))
	now := time.Now()
	for _, item := range chosen {
		course := bykcMap(item["courseInfo"])
		signConfig := parseBykcSignConfig(bykcString(course, "courseSignConfig"))
		checkin := bykcIntPtr(item, "checkin")
		pass := bykcIntPtr(item, "pass")
		canSign, canSignOut := bykcAttendanceAvailability(signConfig, checkin, pass, now)
		result = append(result, dto.BykcChosenCourseDto{
			ID:                     int64(bykcInt(item, "id")),
			CourseID:               int64(bykcInt(course, "id")),
			CourseName:             firstNonBlank(bykcString(course, "courseName"), "未知课程"),
			CoursePosition:         stringPtrNonBlank(bykcString(course, "coursePosition")),
			CourseTeacher:          stringPtrNonBlank(bykcString(course, "courseTeacher")),
			CourseStartDate:        bykcDatePtr(bykcString(course, "courseStartDate")),
			CourseEndDate:          bykcDatePtr(bykcString(course, "courseEndDate")),
			SelectDate:             bykcDatePtr(bykcString(item, "selectDate")),
			CourseCancelEndDate:    bykcDatePtr(bykcString(course, "courseCancelEndDate")),
			Category:               bykcCategoryPtr(bykcString(bykcMap(course["courseNewKind1"]), "kindName")),
			SubCategory:            bykcSubCategoryPtr(bykcString(bykcMap(course["courseNewKind2"]), "kindName")),
			Checkin:                bykcIntDefault(item, "checkin", 0),
			Score:                  bykcIntPtr(item, "score"),
			Pass:                   pass,
			CanSign:                canSign,
			CanSignOut:             canSignOut,
			SignConfig:             signConfig,
			CourseSignType:         bykcIntPtr(course, "courseSignType"),
			Homework:               stringPtrNonBlank(bykcString(item, "homework")),
			HomeworkAttachmentName: stringPtrNonBlank(bykcString(item, "homeworkAttachmentName")),
			HomeworkAttachmentPath: stringPtrNonBlank(bykcString(item, "homeworkAttachmentPath")),
			SignInfo:               stringPtrNonBlank(bykcString(item, "signInfo")),
		})
	}
	return result, nil
}

func (s *Service) GetBykcCourseDetail(ctx context.Context, username string, courseID int64) (dto.BykcCourseDetailDto, error) {
	client := s.bykcClient(username)
	course, err := client.courseByID(ctx, courseID)
	if err != nil {
		return dto.BykcCourseDetailDto{}, err
	}
	now := time.Now()
	status := bykcCourseStatus(course, now)
	signConfig := parseBykcSignConfig(bykcString(course, "courseSignConfig"))
	var checkin *int
	var pass *int
	if bykcBool(course, "selected") {
		if chosen, _ := s.findBykcChosenCourseForCurrentSemester(ctx, client, courseID); chosen != nil {
			if signConfig == nil {
				signConfig = parseBykcSignConfig(bykcString(bykcMap(chosen["courseInfo"]), "courseSignConfig"))
			}
			checkin = bykcIntPtr(chosen, "checkin")
			pass = bykcIntPtr(chosen, "pass")
		}
	}
	canSign, canSignOut := bykcAttendanceAvailability(signConfig, checkin, pass, now)
	return mapBykcCourseDetail(course, status, signConfig, checkin, pass, canSign, canSignOut), nil
}

func (s *Service) SignBykcCourse(ctx context.Context, username string, courseID int64, request dto.BykcSignRequest) (string, error) {
	client := s.bykcClient(username)
	chosen, err := s.findBykcChosenCourseForCurrentSemester(ctx, client, courseID)
	if err != nil {
		return "", err
	}
	if chosen == nil {
		return "", bykcMessage(ErrBykcBusiness, "该课程未选，无法签到")
	}
	course := bykcMap(chosen["courseInfo"])
	signConfig := parseBykcSignConfig(bykcString(course, "courseSignConfig"))
	if signConfig == nil {
		if detail, err := client.courseByID(ctx, courseID); err == nil {
			signConfig = parseBykcSignConfig(bykcString(detail, "courseSignConfig"))
		}
	}
	checkin := bykcIntPtr(chosen, "checkin")
	pass := bykcIntPtr(chosen, "pass")
	canSign, canSignOut := bykcAttendanceAvailability(signConfig, checkin, pass, time.Now())
	if request.SignType == 1 {
		if !canSign {
			return "", bykcMessage(ErrBykcBusiness, "当前不在签到时间窗口")
		}
	} else if !canSignOut {
		return "", bykcMessage(ErrBykcBusiness, "当前不在签退时间窗口")
	}
	lat, lng, err := bykcSignLocation(signConfig, request.Lat, request.Lng)
	if err != nil {
		return "", err
	}
	payload := fmt.Sprintf(`{"courseId":%d,"signLat":%s,"signLng":%s,"signType":%d}`, courseID, strconv.FormatFloat(lat, 'f', -1, 64), strconv.FormatFloat(lng, 'f', -1, 64), request.SignType)
	if err := client.callAPI(ctx, "signCourseByUser", payload, nil); err != nil {
		return "", err
	}
	if request.SignType == 1 {
		return "签到成功", nil
	}
	return "签退成功", nil
}

func (s *Service) GetBykcStatistics(ctx context.Context, username string) (dto.BykcStatisticsDto, error) {
	raw, err := s.bykcClient(username).statistics(ctx)
	if err != nil {
		return dto.BykcStatisticsDto{}, err
	}
	statistical := bykcMap(raw["statistical"])
	categories := []dto.BykcCategoryStatisticsDto{}
	categoryKeys := make([]string, 0, len(statistical))
	for key := range statistical {
		categoryKeys = append(categoryKeys, key)
	}
	sort.Strings(categoryKeys)
	for _, catKey := range categoryKeys {
		subMap := bykcMap(statistical[catKey])
		subKeys := make([]string, 0, len(subMap))
		for key := range subMap {
			subKeys = append(subKeys, key)
		}
		sort.Strings(subKeys)
		for _, subKey := range subKeys {
			stats := bykcMap(subMap[subKey])
			required := bykcInt(stats, "assessmentCount")
			passed := bykcInt(stats, "completeAssessmentCount")
			categories = append(categories, dto.BykcCategoryStatisticsDto{
				CategoryName:    strings.TrimPrefix(catKey[strings.LastIndex(catKey, "|")+1:], "|"),
				SubCategoryName: strings.TrimPrefix(subKey[strings.LastIndex(subKey, "|")+1:], "|"),
				RequiredCount:   required,
				PassedCount:     passed,
				IsQualified:     passed >= required,
			})
		}
	}
	return dto.BykcStatisticsDto{TotalValidCount: bykcInt(raw, "validCount"), Categories: categories}, nil
}

func (s *Service) findBykcChosenCourseForCurrentSemester(ctx context.Context, client *bykcClient, courseID int64) (map[string]any, error) {
	config, err := client.allConfig(ctx)
	if err != nil {
		return nil, err
	}
	semester := resolveBykcCurrentSemester(config, time.Now())
	if semester == nil {
		return nil, bykcMessage(ErrBykcBusiness, "无法获取当前学期信息")
	}
	chosen, err := client.chosenCourses(ctx, bykcString(semester, "semesterStartDate"), bykcString(semester, "semesterEndDate"))
	if err != nil {
		return nil, err
	}
	for _, item := range chosen {
		if int64(bykcInt(bykcMap(item["courseInfo"]), "id")) == courseID {
			return item, nil
		}
	}
	return nil, nil
}

func (s *Service) bykcClient(username string) *bykcClient {
	now := time.Now()
	if raw, ok := s.bykcClients.Load(username); ok {
		cached := raw.(*cachedBykcClient)
		cached.lastAccessAt = now
		return cached.client
	}
	client := &bykcClient{username: username, service: s}
	cached := &cachedBykcClient{client: client, lastAccessAt: now}
	actual, _ := s.bykcClients.LoadOrStore(username, cached)
	return actual.(*cachedBykcClient).client
}

type bykcClient struct {
	username string
	service  *Service
	token    string
}

type bykcAPIResponse struct {
	Status  string          `json:"status"`
	Success *bool           `json:"success"`
	Data    json.RawMessage `json:"data"`
	Msg     string          `json:"msg"`
	Errmsg  string          `json:"errmsg"`
}

func (c *bykcClient) coursePage(ctx context.Context, page, size int) (map[string]any, error) {
	var raw map[string]any
	request := fmt.Sprintf(`{"pageNumber":%d,"pageSize":%d}`, page, size)
	return raw, c.callAPI(ctx, "queryStudentSemesterCourseByPage", request, &raw)
}

func (c *bykcClient) allConfig(ctx context.Context) (map[string]any, error) {
	var raw map[string]any
	return raw, c.callAPI(ctx, "getAllConfig", `{}`, &raw)
}

func (c *bykcClient) chosenCourses(ctx context.Context, startDate, endDate string) ([]map[string]any, error) {
	var raw map[string]any
	request := fmt.Sprintf(`{"startDate":%q,"endDate":%q}`, startDate, endDate)
	if err := c.callAPI(ctx, "queryChosenCourse", request, &raw); err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for _, item := range bykcSlice(raw["courseList"]) {
		if mapped := bykcMap(item); mapped != nil {
			result = append(result, mapped)
		}
	}
	return result, nil
}

func (c *bykcClient) courseByID(ctx context.Context, courseID int64) (map[string]any, error) {
	var raw map[string]any
	request := fmt.Sprintf(`{"id":%d}`, courseID)
	return raw, c.callAPI(ctx, "queryCourseById", request, &raw)
}

func (c *bykcClient) statistics(ctx context.Context) (map[string]any, error) {
	var raw map[string]any
	return raw, c.callAPI(ctx, "queryStatisticByUserId", `{}`, &raw)
}

func (c *bykcClient) callAPI(ctx context.Context, apiName, requestJSON string, out any) error {
	if err := c.login(ctx, false); err != nil {
		return err
	}
	raw, err := c.callAPIRaw(ctx, apiName, requestJSON)
	if err != nil {
		if err := c.login(ctx, true); err != nil {
			return err
		}
		raw, err = c.callAPIRaw(ctx, apiName, requestJSON)
		if err != nil {
			return err
		}
	}
	var response bykcAPIResponse
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return ErrBykcUpstream
	}
	if response.Status != "0" {
		message := firstNonBlank(response.Errmsg, response.Msg, "BYKC request failed")
		return bykcMessage(ErrBykcBusiness, message)
	}
	if out == nil || len(response.Data) == 0 || string(response.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(response.Data, out); err != nil {
		return ErrBykcUpstream
	}
	c.appendParsedRawCourseLog(apiName, out)
	return nil
}

func (c *bykcClient) login(ctx context.Context, force bool) error {
	if !force && strings.TrimSpace(c.token) != "" {
		return nil
	}
	sessionClient, err := c.service.clients.Get(c.username)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.service.clients.UpstreamURL("https://bykc.buaa.edu.cn/sscv/cas/login"), nil)
	if err != nil {
		return err
	}
	resp, err := sessionClient.Client.Do(req)
	if err != nil {
		return upstreamRequestError(err, ErrBykcUpstream, "bykc_timeout")
	}
	location := resp.Header.Get("Location")
	finalURL := resp.Request.URL.String()
	_ = resp.Body.Close()
	token := extractBykcToken(finalURL)
	if token == "" {
		token = extractBykcToken(location)
	}
	if token != "" {
		c.token = token
		return nil
	}
	fallback, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.service.clients.UpstreamURL("https://bykc.buaa.edu.cn/cas-login?token="), nil)
	if fallback != nil {
		if fallbackResp, err := sessionClient.Client.Do(fallback); err == nil {
			_ = fallbackResp.Body.Close()
		}
	}
	return nil
}

func (c *bykcClient) callAPIRaw(ctx context.Context, apiName, requestJSON string) (string, error) {
	sessionClient, err := c.service.clients.Get(c.username)
	if err != nil {
		return "", err
	}
	encrypted, err := encryptBykcRequest(requestJSON)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.service.clients.UpstreamURL("https://bykc.buaa.edu.cn/sscv/"+apiName), strings.NewReader(encrypted.EncryptedData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", c.service.clients.UpstreamURL("https://bykc.buaa.edu.cn/system/course-select"))
	req.Header.Set("Origin", c.service.clients.UpstreamURL("https://bykc.buaa.edu.cn"))
	if c.token != "" {
		req.Header.Set("auth_token", c.token)
		req.Header.Set("authtoken", c.token)
	}
	req.Header.Set("ak", encrypted.AK)
	req.Header.Set("sk", encrypted.SK)
	req.Header.Set("ts", encrypted.TS)
	resp, err := sessionClient.Client.Do(req)
	if err != nil {
		return "", upstreamRequestError(err, ErrBykcUpstream, "bykc_timeout")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", upstreamStatusError(resp.StatusCode, ErrBykcUpstream, "bykc_timeout")
	}
	decoded := decodeBykcResponse(string(body), encrypted.AESKey)
	c.appendRawLog(apiName, requestJSON, decoded)
	if strings.Contains(decoded, "会话已失效") || strings.Contains(decoded, "未登录") {
		return "", ErrFeatureUnauthenticated
	}
	return decoded, nil
}

func (c *bykcClient) appendRawLog(apiName, requestJSON, raw string) {
	if !c.service.bykcDebugRawAPILog {
		return
	}
	log.Printf("BYKC raw api log user=%s api=%s request=%s raw=%s", c.username, apiName, requestJSON, raw)
}

func (c *bykcClient) appendParsedRawCourseLog(apiName string, out any) {
	if !c.service.bykcDebugParsedCourseLog || out == nil {
		return
	}
	var courses []dto.BykcCourseDto
	switch typed := out.(type) {
	case *map[string]any:
		if typed == nil {
			return
		}
		for _, key := range []string{"content", "courseList"} {
			for _, item := range bykcSlice((*typed)[key]) {
				course := bykcMap(item)
				if nested := bykcMap(course["courseInfo"]); nested != nil {
					course = nested
				}
				if course != nil {
					courses = append(courses, mapBykcCourse(course, bykcCourseStatus(course, time.Now())))
				}
			}
		}
	case *[]dto.BykcChosenCourseDto:
		if typed == nil {
			return
		}
		raw, err := json.Marshal(typed)
		if err == nil {
			log.Printf("BYKC parsed course log user=%s api=%s count=%d raw=%s", c.username, apiName, len(*typed), string(raw))
		}
		return
	default:
		return
	}
	if len(courses) == 0 {
		return
	}
	raw, err := json.Marshal(courses)
	if err != nil {
		return
	}
	log.Printf("BYKC parsed course log user=%s api=%s count=%d raw=%s", c.username, apiName, len(courses), string(raw))
}

func mapBykcCourse(raw map[string]any, status string) dto.BykcCourseDto {
	return dto.BykcCourseDto{
		ID:                    int64(bykcInt(raw, "id")),
		CourseName:            strings.TrimSpace(bykcString(raw, "courseName")),
		CoursePosition:        stringPtrNonBlank(bykcString(raw, "coursePosition")),
		CourseTeacher:         stringPtrNonBlank(bykcString(raw, "courseTeacher")),
		CourseStartDate:       bykcDatePtr(bykcString(raw, "courseStartDate")),
		CourseEndDate:         bykcDatePtr(bykcString(raw, "courseEndDate")),
		CourseSelectStartDate: bykcDatePtr(bykcString(raw, "courseSelectStartDate")),
		CourseSelectEndDate:   bykcDatePtr(bykcString(raw, "courseSelectEndDate")),
		CourseCancelEndDate:   bykcDatePtr(bykcString(raw, "courseCancelEndDate")),
		CourseMaxCount:        bykcInt(raw, "courseMaxCount"),
		CourseCurrentCount:    bykcIntPtr(raw, "courseCurrentCount"),
		Category:              bykcCategoryPtr(bykcString(bykcMap(raw["courseNewKind1"]), "kindName")),
		SubCategory:           bykcSubCategoryPtr(bykcString(bykcMap(raw["courseNewKind2"]), "kindName")),
		AudienceCampuses:      bykcAudienceCampusLabels(raw),
		HasSignPoints:         bykcHasSignPoints(bykcString(raw, "courseSignConfig")),
		Status:                status,
		Selected:              bykcBool(raw, "selected"),
	}
}

func mapBykcCourseDetail(raw map[string]any, status string, signConfig *dto.BykcSignConfigDto, checkin, pass *int, canSign, canSignOut bool) dto.BykcCourseDetailDto {
	base := mapBykcCourse(raw, status)
	base.HasSignPoints = signConfig != nil && len(signConfig.SignPoints) > 0
	base.AudienceCampuses = bykcAudienceCampusLabels(raw)
	return dto.BykcCourseDetailDto{
		BykcCourseDto:       base,
		CourseContact:       stringPtrNonBlank(bykcString(raw, "courseContact")),
		CourseContactMobile: stringPtrNonBlank(bykcString(raw, "courseContactMobile")),
		OrganizerCollegeName: stringPtrNonBlank(
			bykcString(bykcMap(raw["courseBelongCollege"]), "collegeName"),
		),
		CourseDesc:       stringPtrNonBlank(bykcString(raw, "courseDesc")),
		AudienceColleges: bykcStringList(raw["courseCollegeList"]),
		AudienceTerms:    bykcStringList(raw["courseTermList"]),
		AudienceGroups:   bykcStringList(raw["courseGroupList"]),
		SignConfig:       signConfig,
		Checkin:          checkin,
		Pass:             pass,
		CanSign:          canSign,
		CanSignOut:       canSignOut,
	}
}

func bykcCourseStatus(course map[string]any, now time.Time) string {
	courseStart := parseBykcDate(bykcString(course, "courseStartDate"))
	selectStart := parseBykcDate(bykcString(course, "courseSelectStartDate"))
	selectEnd := parseBykcDate(bykcString(course, "courseSelectEndDate"))
	switch {
	case courseStart != nil && now.After(*courseStart):
		return bykcStatusExpired
	case bykcBool(course, "selected"):
		return bykcStatusSelected
	case selectEnd != nil && now.After(*selectEnd):
		return bykcStatusEnded
	case bykcIntPtr(course, "courseCurrentCount") != nil && bykcInt(course, "courseCurrentCount") >= bykcInt(course, "courseMaxCount"):
		return bykcStatusFull
	case selectStart != nil && now.Before(*selectStart):
		return bykcStatusPreview
	default:
		return bykcStatusAvailable
	}
}

func parseBykcSignConfig(raw string) *dto.BykcSignConfigDto {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var parsed struct {
		SignStartDate    string `json:"signStartDate"`
		SignEndDate      string `json:"signEndDate"`
		SignOutStartDate string `json:"signOutStartDate"`
		SignOutEndDate   string `json:"signOutEndDate"`
		SignPointList    []struct {
			Lat    float64 `json:"lat"`
			Lng    float64 `json:"lng"`
			Radius float64 `json:"radius"`
		} `json:"signPointList"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	points := make([]dto.BykcSignPointDto, 0, len(parsed.SignPointList))
	for _, point := range parsed.SignPointList {
		points = append(points, dto.BykcSignPointDto{Lat: point.Lat, Lng: point.Lng, Radius: point.Radius})
	}
	return &dto.BykcSignConfigDto{
		SignStartDate:    bykcDatePtr(parsed.SignStartDate),
		SignEndDate:      bykcDatePtr(parsed.SignEndDate),
		SignOutStartDate: bykcDatePtr(parsed.SignOutStartDate),
		SignOutEndDate:   bykcDatePtr(parsed.SignOutEndDate),
		SignPoints:       points,
	}
}

func bykcAttendanceAvailability(signConfig *dto.BykcSignConfigDto, checkin, pass *int, now time.Time) (bool, bool) {
	passed := pass != nil && *pass == 1
	canSign := !passed && bykcUnsignedCheckin(checkin) && bykcWithinWindow(signConfigDate(signConfig, "signStart"), signConfigDate(signConfig, "signEnd"), now)
	canSignOut := !passed && bykcEligibleForSignOut(checkin) && bykcWithinWindow(signConfigDate(signConfig, "signOutStart"), signConfigDate(signConfig, "signOutEnd"), now)
	return canSign, canSignOut
}

func bykcSignLocation(signConfig *dto.BykcSignConfigDto, fallbackLat, fallbackLng *float64) (float64, float64, error) {
	if signConfig != nil && len(signConfig.SignPoints) > 0 {
		point := signConfig.SignPoints[mrand.Intn(len(signConfig.SignPoints))]
		if point.Radius > 0 {
			dist := point.Radius * math.Sqrt(mrand.Float64())
			angle := mrand.Float64() * 2 * math.Pi
			lat, lng := bykcDestinationPoint(point.Lat, point.Lng, dist, angle)
			return lat, lng, nil
		}
	}
	if fallbackLat != nil && fallbackLng != nil {
		return *fallbackLat, *fallbackLng, nil
	}
	return 0, 0, bykcMessage(ErrBykcBusiness, "未提供签到坐标且后端未返回签到范围")
}

func bykcDestinationPoint(lat, lng, dist, angle float64) (float64, float64) {
	r := dist / 6371000.0
	lr := lat * math.Pi / 180
	gr := lng * math.Pi / 180
	dLat := math.Asin(math.Sin(lr)*math.Cos(r) + math.Cos(lr)*math.Sin(r)*math.Cos(angle))
	dLng := gr + math.Atan2(math.Sin(angle)*math.Sin(r)*math.Cos(lr), math.Cos(r)-math.Sin(lr)*math.Sin(dLat))
	return dLat * 180 / math.Pi, dLng * 180 / math.Pi
}

func resolveBykcCurrentSemester(config map[string]any, now time.Time) map[string]any {
	semesters := []map[string]any{}
	for _, item := range bykcSlice(config["semester"]) {
		if semester := bykcMap(item); semester != nil {
			semesters = append(semesters, semester)
		}
	}
	for _, semester := range semesters {
		if bykcWithinWindow(parseBykcDate(bykcString(semester, "semesterStartDate")), parseBykcDate(bykcString(semester, "semesterEndDate")), now) {
			return semester
		}
	}
	var latest map[string]any
	var latestTime time.Time
	for _, semester := range semesters {
		end := parseBykcDate(bykcString(semester, "semesterEndDate"))
		if end != nil && (latest == nil || end.After(latestTime)) {
			latest = semester
			latestTime = *end
		}
	}
	return latest
}

type encryptedBykcRequest struct {
	EncryptedData string
	AK            string
	SK            string
	TS            string
	AESKey        []byte
}

func encryptBykcRequest(jsonData string) (encryptedBykcRequest, error) {
	aesKey := generateBykcAESKey()
	data := []byte(jsonData)
	ak, err := rsaEncryptBykc(aesKey)
	if err != nil {
		return encryptedBykcRequest{}, err
	}
	hash := sha1.Sum(data)
	sk, err := rsaEncryptBykc([]byte(hex.EncodeToString(hash[:])))
	if err != nil {
		return encryptedBykcRequest{}, err
	}
	encryptedData, err := aesECBEncrypt(data, aesKey)
	if err != nil {
		return encryptedBykcRequest{}, err
	}
	return encryptedBykcRequest{
		EncryptedData: base64.StdEncoding.EncodeToString(encryptedData),
		AK:            ak,
		SK:            sk,
		TS:            strconv.FormatInt(time.Now().UnixMilli(), 10),
		AESKey:        aesKey,
	}, nil
}

func decodeBykcResponse(raw string, aesKey []byte) string {
	respBase64 := strings.TrimSpace(raw)
	var quoted string
	if err := json.Unmarshal([]byte(respBase64), &quoted); err == nil {
		respBase64 = quoted
	}
	encrypted, err := base64.StdEncoding.DecodeString(respBase64)
	if err != nil {
		return respBase64
	}
	decrypted, err := aesECBDecrypt(encrypted, aesKey)
	if err != nil {
		return respBase64
	}
	return string(decrypted)
}

func rsaEncryptBykc(data []byte) (string, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(bykcRSAPublicKeyBase64)
	if err != nil {
		return "", err
	}
	parsed, err := x509.ParsePKIXPublicKey(keyBytes)
	if err != nil {
		return "", err
	}
	publicKey, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return "", ErrBykcUpstream
	}
	encrypted, err := rsa.EncryptPKCS1v15(crand.Reader, publicKey, data)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

func generateBykcAESKey() []byte {
	key := make([]byte, bykcAESKeyLength)
	maxValue := big.NewInt(int64(len(bykcKeyChars)))
	for i := range key {
		index, err := crand.Int(crand.Reader, maxValue)
		if err != nil {
			key[i] = bykcKeyChars[mrand.Intn(len(bykcKeyChars))]
			continue
		}
		key[i] = bykcKeyChars[index.Int64()]
	}
	return key
}

func aesECBEncrypt(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(data, aes.BlockSize)
	out := make([]byte, len(padded))
	for start := 0; start < len(padded); start += aes.BlockSize {
		block.Encrypt(out[start:start+aes.BlockSize], padded[start:start+aes.BlockSize])
	}
	return out, nil
}

func aesECBDecrypt(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, ErrBykcUpstream
	}
	out := make([]byte, len(data))
	for start := 0; start < len(data); start += aes.BlockSize {
		block.Decrypt(out[start:start+aes.BlockSize], data[start:start+aes.BlockSize])
	}
	return pkcs7Unpad(out, aes.BlockSize)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, ErrBykcUpstream
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > blockSize || pad > len(data) {
		return nil, ErrBykcUpstream
	}
	for _, value := range data[len(data)-pad:] {
		if int(value) != pad {
			return nil, ErrBykcUpstream
		}
	}
	return data[:len(data)-pad], nil
}

func bykcError(c *fiber.Ctx, err error) error {
	if code, ok := timeoutCode(err); ok {
		return httpx.Error(c, fiber.StatusGatewayTimeout, code)
	}
	switch {
	case errors.Is(err, ErrFeatureUnauthenticated):
		return httpx.Error(c, fiber.StatusUnauthorized, "unauthenticated")
	case errors.Is(err, ErrBykcInvalidRequest):
		return httpx.Error(c, fiber.StatusBadRequest, "invalid_request")
	case errors.Is(err, ErrBykcBusiness), errors.Is(err, ErrBykcUpstream):
		return httpx.Error(c, fiber.StatusBadGateway, "bykc_error")
	default:
		return httpx.Error(c, fiber.StatusInternalServerError, "internal_server_error")
	}
}

func bykcMessage(err error, message string) error {
	return bykcErrorWithMessage{err: err, message: message}
}

func extractBykcToken(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if index := strings.Index(raw, "?token="); index >= 0 {
		return raw[index+len("?token="):]
	}
	return ""
}

func bykcDatePtr(raw string) *string {
	parsed := parseBykcDate(raw)
	if parsed == nil {
		return nil
	}
	text := parsed.Format("2006-01-02 15:04:05")
	return &text
}

func parseBykcDate(raw string) *time.Time {
	normalized := strings.TrimSpace(strings.ReplaceAll(raw, "T", " "))
	if normalized == "" {
		return nil
	}
	layouts := []string{"2006-01-02 15:04:05", "2006-01-02 15:04", time.RFC3339}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return &parsed
		}
		if parsed, err := time.ParseInLocation(layout, normalized, time.Local); err == nil {
			return &parsed
		}
	}
	return nil
}

func bykcWithinWindow(start, end *time.Time, now time.Time) bool {
	return start != nil && end != nil && !now.Before(*start) && !now.After(*end)
}

func signConfigDate(config *dto.BykcSignConfigDto, field string) *time.Time {
	if config == nil {
		return nil
	}
	switch field {
	case "signStart":
		if config.SignStartDate != nil {
			return parseBykcDate(*config.SignStartDate)
		}
	case "signEnd":
		if config.SignEndDate != nil {
			return parseBykcDate(*config.SignEndDate)
		}
	case "signOutStart":
		if config.SignOutStartDate != nil {
			return parseBykcDate(*config.SignOutStartDate)
		}
	case "signOutEnd":
		if config.SignOutEndDate != nil {
			return parseBykcDate(*config.SignOutEndDate)
		}
	}
	return nil
}

func bykcUnsignedCheckin(checkin *int) bool {
	return checkin == nil || *checkin == 0
}

func bykcEligibleForSignOut(checkin *int) bool {
	return bykcUnsignedCheckin(checkin) || (checkin != nil && (*checkin == 5 || *checkin == 6))
}

func bykcAudienceCampusLabels(raw map[string]any) []string {
	labels := bykcStringList(raw["courseCampusList"])
	result := []string{}
	seen := map[string]bool{}
	for _, label := range labels {
		if label == "全部校区" {
			label = "未指定校区"
		}
		if label != "" && !seen[label] {
			result = append(result, label)
			seen[label] = true
		}
	}
	if len(result) > 0 {
		return result
	}
	if strings.TrimSpace(bykcString(raw, "courseCampus")) == "ALL" {
		return []string{"未指定校区"}
	}
	return []string{}
}

func bykcHasSignPoints(raw string) bool {
	config := parseBykcSignConfig(raw)
	return config != nil && len(config.SignPoints) > 0
}

func bykcCategoryPtr(raw string) *string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}
	if text != "博雅课程" {
		text = "未知分类"
	}
	return &text
}

func bykcSubCategoryPtr(raw string) *string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}
	valid := map[string]bool{"德育": true, "美育": true, "劳动教育": true, "安全健康": true, "其他方面": true}
	if !valid[text] {
		text = "未知类型"
	}
	return &text
}

func bykcString(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	return bykcAnyString(raw[key])
}

func bykcAnyString(value any) string {
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
	case bool:
		return strconv.FormatBool(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func bykcInt(raw map[string]any, key string) int {
	return bykcIntAny(raw[key], 0)
}

func bykcIntDefault(raw map[string]any, key string, fallback int) int {
	return bykcIntAny(raw[key], fallback)
}

func bykcIntAny(value any, fallback int) int {
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

func bykcIntPtr(raw map[string]any, key string) *int {
	if raw == nil || raw[key] == nil {
		return nil
	}
	value := bykcInt(raw, key)
	return &value
}

func bykcBool(raw map[string]any, key string) bool {
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

func bykcMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func bykcSlice(value any) []any {
	if typed, ok := value.([]any); ok {
		return typed
	}
	return nil
}

func bykcStringList(value any) []string {
	result := []string{}
	for _, item := range bykcSlice(value) {
		if text := bykcAnyString(item); text != "" {
			result = append(result, text)
		}
	}
	return result
}

const (
	bykcStatusExpired   = "已过期"
	bykcStatusSelected  = "已选"
	bykcStatusPreview   = "预告"
	bykcStatusEnded     = "选课结束"
	bykcStatusFull      = "人数已满"
	bykcStatusAvailable = "可选"

	bykcRSAPublicKeyBase64 = "MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDlHMQ3B5GsWnCe7Nlo1YiG/YmHdlOiKOST5aRm4iaqYSvhvWmwcigoyWTM+8bv2+sf6nQBRDWTY4KmNV7DBk1eDnTIQo6ENA31k5/tYCLEXgjPbEjCK9spiyB62fCT6cqOhbamJB0lcDJRO6Vo1m3dy+fD0jbxfDVBBNtyltIsDQIDAQAB"
	bykcKeyChars           = "ABCDEFGHJKMNPQRSTWXYZabcdefhijkmnprstwxyz2345678"
	bykcAESKeyLength       = 16
)
