package features

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BUAASubnet/UBAA.Server/internal/dto"
	"github.com/BUAASubnet/UBAA.Server/internal/upstream"
)

var (
	ErrFeatureUnauthenticated = errors.New("feature unauthenticated")
	ErrFeatureUpstream        = errors.New("feature upstream error")
	ErrUnsupportedPortal      = errors.New("unsupported portal")
	ErrSpocAuth               = errors.New("spoc auth failed")
	ErrJudgeAuth              = errors.New("judge auth failed")
)

type Service struct {
	clients                  *upstream.ClientFactory
	bykcDebugRawAPILog       bool
	bykcDebugParsedCourseLog bool
	signinClients            sync.Map
	spocClients              sync.Map
	judgeClients             sync.Map
	libBookClients           sync.Map
	cgyyClients              sync.Map
	bykcClients              sync.Map
	ygdkClients              sync.Map
	ygdkContexts             sync.Map
}

type Options struct {
	BykcDebugRawAPILog       bool
	BykcDebugParsedCourseLog bool
}

func NewService(clients *upstream.ClientFactory, options ...Options) *Service {
	opts := Options{}
	if len(options) > 0 {
		opts = options[0]
	}
	return &Service{
		clients:                  clients,
		bykcDebugRawAPILog:       opts.BykcDebugRawAPILog,
		bykcDebugParsedCourseLog: opts.BykcDebugParsedCourseLog,
	}
}

type cachedSigninClient struct {
	client       *signinClient
	lastAccessAt time.Time
}

type cachedSpocClient struct {
	client       *spocClient
	lastAccessAt time.Time
}

type cachedJudgeClient struct {
	client       *judgeClient
	lastAccessAt time.Time
}

type cachedLibBookClient struct {
	client       *libBookClient
	lastAccessAt time.Time
}

type cachedCgyyClient struct {
	client       *cgyyClient
	lastAccessAt time.Time
}

type cachedBykcClient struct {
	client       *bykcClient
	lastAccessAt time.Time
}

type cachedYgdkClient struct {
	client       *ygdkClient
	lastAccessAt time.Time
}

type CacheSizes struct {
	Signin      int
	Bykc        int
	Cgyy        int
	Spoc        int
	Judge       int
	LibBook     int
	Ygdk        int
	YgdkContext int
}

func (s *Service) CacheSizes() CacheSizes {
	if s == nil {
		return CacheSizes{}
	}
	return CacheSizes{
		Signin:      syncMapLen(&s.signinClients),
		Bykc:        syncMapLen(&s.bykcClients),
		Cgyy:        syncMapLen(&s.cgyyClients),
		Spoc:        syncMapLen(&s.spocClients),
		Judge:       syncMapLen(&s.judgeClients),
		LibBook:     syncMapLen(&s.libBookClients),
		Ygdk:        syncMapLen(&s.ygdkClients),
		YgdkContext: syncMapLen(&s.ygdkContexts),
	}
}

func (s *Service) FetchTerms(ctx context.Context, username string) ([]dto.Term, error) {
	if err := s.ensureUndergradPortal(ctx, username); err != nil {
		return nil, err
	}
	var response dto.TermResponse
	if err := s.getJSON(ctx, username, "https://byxt.buaa.edu.cn/jwapp/sys/homeapp/api/home/student/schoolCalendars.do", scheduleHeaders(), &response); err != nil {
		return nil, err
	}
	if response.Code != "0" {
		return nil, ErrFeatureUpstream
	}
	return response.Datas, nil
}

func syncMapLen(m *sync.Map) int {
	count := 0
	m.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func (s *Service) FetchWeeks(ctx context.Context, username, termCode string) ([]dto.Week, error) {
	if err := s.ensureUndergradPortal(ctx, username); err != nil {
		return nil, err
	}
	target := "https://byxt.buaa.edu.cn/jwapp/sys/homeapp/api/home/getTermWeeks.do?termCode=" + url.QueryEscape(termCode)
	var response dto.WeekResponse
	if err := s.getJSON(ctx, username, target, scheduleHeaders(), &response); err != nil {
		return nil, err
	}
	return response.Datas, nil
}

func (s *Service) FetchWeeklySchedule(ctx context.Context, username, termCode string, week int) (dto.WeeklySchedule, error) {
	if err := s.ensureUndergradPortal(ctx, username); err != nil {
		return dto.WeeklySchedule{}, err
	}
	form := url.Values{}
	form.Set("termCode", termCode)
	form.Set("type", "week")
	form.Set("week", strconv.Itoa(week))
	var response dto.WeeklyScheduleResponse
	if err := s.postFormJSON(ctx, username, "https://byxt.buaa.edu.cn/jwapp/sys/homeapp/api/home/student/getMyScheduleDetail.do", scheduleHeaders(), form, &response); err != nil {
		return dto.WeeklySchedule{}, err
	}
	if response.Code != "0" {
		return dto.WeeklySchedule{}, ErrFeatureUpstream
	}
	return response.Datas, nil
}

func (s *Service) FetchTodaySchedule(ctx context.Context, username string) ([]dto.TodayClass, error) {
	if err := s.ensureUndergradPortal(ctx, username); err != nil {
		return nil, err
	}
	today := time.Now().Format("2006-01-02")
	target := "https://byxt.buaa.edu.cn/jwapp/sys/homeapp/api/home/teachingSchedule/detail.do?rq=" + url.QueryEscape(today) + "&lxdm=student"
	var response dto.TodayScheduleResponse
	if err := s.getJSON(ctx, username, target, scheduleHeaders(), &response); err != nil {
		return nil, err
	}
	if response.Code != "0" {
		return nil, ErrFeatureUpstream
	}
	return response.Datas, nil
}

func (s *Service) FetchExams(ctx context.Context, username, termCode string) (dto.ExamArrangementData, error) {
	if err := s.ensureUndergradPortal(ctx, username); err != nil {
		return dto.ExamArrangementData{}, err
	}
	target := "https://byxt.buaa.edu.cn/jwapp/sys/homeapp/api/home/student/exams.do?termCode=" + url.QueryEscape(termCode)
	var response struct {
		Code  string     `json:"code"`
		Msg   *string    `json:"msg"`
		Datas []dto.Exam `json:"datas"`
	}
	if err := s.getJSON(ctx, username, target, examHeaders(), &response); err != nil {
		return dto.ExamArrangementData{}, err
	}
	if response.Code != "0" {
		return dto.ExamArrangementData{}, ErrFeatureUpstream
	}
	return dto.ExamArrangementData{Arranged: response.Datas, NotArranged: []dto.Exam{}}, nil
}

func (s *Service) FetchGrades(ctx context.Context, username, termCode string) (dto.GradeData, error) {
	term, semester, err := parseTermCode(termCode)
	if err != nil {
		return dto.GradeData{}, err
	}
	if _, err := s.do(ctx, username, http.MethodGet, buaaScoreURL, gradePageHeaders(), nil); err != nil {
		return dto.GradeData{}, err
	}
	form := url.Values{}
	form.Set("xq", strconv.Itoa(semester))
	form.Set("year", term)
	var response struct {
		Code    int     `json:"e"`
		Message *string `json:"m"`
		Data    map[string]struct {
			CourseName *string         `json:"kcmc"`
			CourseCode *string         `json:"kch"`
			Credit     json.RawMessage `json:"xf"`
			Score      json.RawMessage `json:"kccj"`
			ScoreType  *string         `json:"fslx"`
			CourseType *string         `json:"kclx"`
		} `json:"d"`
	}
	if err := s.postFormJSON(ctx, username, buaaScoreURL, gradeQueryHeaders(s.clients.UpstreamURL(buaaScoreURL)), form, &response); err != nil {
		return dto.GradeData{}, err
	}
	if response.Code != 0 {
		return dto.GradeData{}, ErrFeatureUpstream
	}
	grades := make([]dto.Grade, 0, len(response.Data))
	for _, course := range response.Data {
		credit := rawNumber(course.Credit)
		score := rawString(course.Score)
		termName := termCode
		grades = append(grades, dto.Grade{
			TermCode:        &termCode,
			TermName:        &termName,
			CourseName:      cleanPtr(course.CourseName),
			CourseCode:      cleanPtr(course.CourseCode),
			Credit:          credit,
			Score:           score,
			CourseAttribute: cleanPtr(course.CourseType),
			RecognitionType: cleanPtr(course.ScoreType),
		})
	}
	return dto.GradeData{TermCode: termCode, Grades: grades}, nil
}

func (s *Service) QueryClassroom(ctx context.Context, username string, xqid int, date string) (dto.ClassroomQueryResponse, error) {
	loginURL := "https://sso.buaa.edu.cn/login?service=https%3A%2F%2Fapp.buaa.edu.cn%2Fa_buaa%2Fapi%2Fcas%2Findex%3Fredirect%3Dhttps%253A%252F%252Fapp.buaa.edu.cn%252Fsite%252FclassRoomQuery%252Findex%26from%3Dwap%26login_from%3D&noAutoRedirect=1"
	_, _ = s.do(ctx, username, http.MethodGet, loginURL, map[string]string{"User-Agent": classroomUserAgent}, nil)
	target := "https://app.buaa.edu.cn/buaafreeclass/wap/default/search1?xqid=" + strconv.Itoa(xqid) + "&floorid=&date=" + url.QueryEscape(date)
	var response dto.ClassroomQueryResponse
	if err := s.getJSON(ctx, username, target, classroomHeaders(s.clients.UpstreamURL("https://app.buaa.edu.cn/site/classRoomQuery/index")), &response); err != nil {
		return dto.ClassroomQueryResponse{}, err
	}
	return response, nil
}

func (s *Service) GetTodaySigninClasses(ctx context.Context, studentID string) (dto.SigninStatusResponse, error) {
	today := time.Now().Format("20060102")
	classes, err := s.signinClient(studentID).GetClasses(ctx, today)
	if err != nil {
		return dto.SigninStatusResponse{}, err
	}
	return dto.SigninStatusResponse{Code: 200, Message: "获取成功", Data: classes}, nil
}

func (s *Service) PerformSignin(ctx context.Context, studentID, courseID string) (dto.SigninActionResponse, error) {
	success, message, err := s.signinClient(studentID).SignIn(ctx, courseID)
	if err != nil {
		return dto.SigninActionResponse{}, err
	}
	code := 400
	if success {
		code = 200
	}
	return dto.SigninActionResponse{Code: code, Success: success, Message: message}, nil
}

func (s *Service) signinClient(studentID string) *signinClient {
	now := time.Now()
	if raw, ok := s.signinClients.Load(studentID); ok {
		cached := raw.(*cachedSigninClient)
		cached.lastAccessAt = now
		return cached.client
	}
	client := &signinClient{
		studentID: studentID,
		client:    &http.Client{Timeout: 30 * time.Second},
		upstream:  s.clients,
	}
	cached := &cachedSigninClient{client: client, lastAccessAt: now}
	actual, _ := s.signinClients.LoadOrStore(studentID, cached)
	return actual.(*cachedSigninClient).client
}

func (s *Service) getJSON(ctx context.Context, username, target string, headers map[string]string, out any) error {
	body, err := s.do(ctx, username, http.MethodGet, target, headers, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return ErrFeatureUpstream
	}
	return nil
}

func (s *Service) postFormJSON(ctx context.Context, username, target string, headers map[string]string, form url.Values, out any) error {
	if headers == nil {
		headers = map[string]string{}
	}
	headers["Content-Type"] = "application/x-www-form-urlencoded"
	body, err := s.do(ctx, username, http.MethodPost, target, headers, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return ErrFeatureUpstream
	}
	return nil
}

func (s *Service) do(ctx context.Context, username, method, target string, headers map[string]string, body io.Reader) ([]byte, error) {
	resp, raw, err := s.doRaw(ctx, username, method, target, headers, body)
	if err != nil {
		return nil, err
	}
	if isSessionExpired(resp, raw) {
		return nil, ErrFeatureUnauthenticated
	}
	if resp.StatusCode != http.StatusOK {
		return nil, upstreamStatusError(resp.StatusCode, ErrFeatureUpstream, "upstream_timeout")
	}
	return raw, nil
}

func (s *Service) doRaw(ctx context.Context, username, method, target string, headers map[string]string, body io.Reader) (*http.Response, []byte, error) {
	sessionClient, err := s.clients.Get(username)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, s.clients.UpstreamURL(target), body)
	if err != nil {
		return nil, nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := sessionClient.Client.Do(req)
	if err != nil {
		return nil, nil, upstreamRequestError(err, ErrFeatureUpstream, "upstream_timeout")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw, nil
}

func isSessionExpired(resp *http.Response, body []byte) bool {
	if resp.StatusCode == http.StatusUnauthorized {
		return true
	}
	finalURL := resp.Request.URL.String()
	if upstream.IsSSOURL(finalURL) {
		return true
	}
	text := string(body)
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(strings.ToLower(trimmed), "<!doctype html") || strings.HasPrefix(strings.ToLower(trimmed), "<html") {
		return strings.Contains(text, `input name="execution"`) || strings.Contains(text, "统一身份认证")
	}
	return false
}

func (s *Service) ensureUndergradPortal(ctx context.Context, username string) error {
	resp, body, err := s.doRaw(ctx, username, http.MethodGet, byxtCurrentUserURL, portalProbeHeaders(), nil)
	if err != nil {
		return err
	}
	result := classifyUndergradPortal(resp, body)
	if result == academicPortalUndergrad {
		return nil
	}
	if result == academicPortalSSORequired {
		return ErrFeatureUnauthenticated
	}
	resp, body, err = s.doRaw(ctx, username, http.MethodGet, graduateUserInfoURL, scheduleHeaders(), nil)
	if err != nil {
		return err
	}
	switch classifyGraduatePortal(resp, body) {
	case academicPortalGraduate:
		return ErrUnsupportedPortal
	case academicPortalSSORequired:
		return ErrFeatureUnauthenticated
	default:
		return ErrFeatureUpstream
	}
}

type academicPortalResult string

const (
	academicPortalUndergrad   academicPortalResult = "undergrad"
	academicPortalGraduate    academicPortalResult = "graduate"
	academicPortalSSORequired academicPortalResult = "sso_required"
	academicPortalUnavailable academicPortalResult = "unavailable"
)

func classifyUndergradPortal(resp *http.Response, body []byte) academicPortalResult {
	if isSessionExpired(resp, body) {
		return academicPortalSSORequired
	}
	if resp.StatusCode != http.StatusOK {
		return academicPortalUnavailable
	}
	finalURL := upstream.FromWebVPNURL(resp.Request.URL.String())
	text := string(body)
	trimmed := strings.TrimSpace(text)
	if strings.Contains(strings.ToLower(finalURL), "/jwapp/sys/byrhmhsy/") {
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || trimmed == "" {
			return academicPortalGraduate
		}
		return academicPortalUnavailable
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html") {
		return academicPortalUnavailable
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return academicPortalUndergrad
	}
	return academicPortalUnavailable
}

func classifyGraduatePortal(resp *http.Response, body []byte) academicPortalResult {
	if isSessionExpired(resp, body) {
		return academicPortalSSORequired
	}
	if resp.StatusCode != http.StatusOK {
		return academicPortalUnavailable
	}
	var parsed struct {
		Code string `json:"code"`
		Data *struct {
			UserID string `json:"userId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return academicPortalUnavailable
	}
	if parsed.Code == "0" && parsed.Data != nil && strings.TrimSpace(parsed.Data.UserID) != "" {
		return academicPortalGraduate
	}
	return academicPortalUnavailable
}

func portalProbeHeaders() map[string]string {
	return map[string]string{
		"Accept":     "application/json, text/html, */*",
		"User-Agent": "UBAA-Backend/1.0",
	}
}

func scheduleHeaders() map[string]string {
	return map[string]string{
		"Accept":           "application/json, text/javascript, */*; q=0.01",
		"X-Requested-With": "XMLHttpRequest",
		"Referer":          "https://byxt.buaa.edu.cn/jwapp/sys/homeapp/index.html",
	}
}

func examHeaders() map[string]string {
	return map[string]string{
		"Accept":           "*/*",
		"X-Requested-With": "XMLHttpRequest",
		"Referer":          "https://byxt.buaa.edu.cn/jwapp/sys/homeapp/home/index.html",
	}
}

func gradePageHeaders() map[string]string {
	return map[string]string{
		"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	}
}

func gradeQueryHeaders(referrer string) map[string]string {
	return map[string]string{
		"Accept":           "application/json, text/javascript, */*; q=0.01",
		"X-Requested-With": "XMLHttpRequest",
		"Referer":          referrer,
	}
}

func classroomHeaders(referrer string) map[string]string {
	return map[string]string{
		"User-Agent":       classroomUserAgent,
		"Accept":           "application/json, text/javascript, */*; q=0.01",
		"X-Requested-With": "XMLHttpRequest",
		"Referer":          referrer,
	}
}

func parseTermCode(termCode string) (string, int, error) {
	parts := strings.Split(strings.TrimSpace(termCode), "-")
	if len(parts) != 3 {
		return "", 0, ErrFeatureUpstream
	}
	semester, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", 0, ErrFeatureUpstream
	}
	return parts[0] + "-" + parts[1], semester, nil
}

func rawString(raw json.RawMessage) *string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}
		return &text
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		text = strconv.FormatFloat(number, 'f', -1, 64)
		return &text
	}
	return nil
}

func rawNumber(raw json.RawMessage) *float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return &number
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err == nil {
			return &parsed
		}
	}
	return nil
}

func cleanPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

const (
	byxtCurrentUserURL  = "https://byxt.buaa.edu.cn/jwapp/sys/homeapp/api/home/currentUser.do"
	graduateUserInfoURL = "https://gsmis.buaa.edu.cn/gsapp/sys/yjsemaphome/modules/pubWork/getUserInfo.do"
	buaaScoreURL        = "https://app.buaa.edu.cn/buaascore/wap/default/index"
	classroomUserAgent  = "Mozilla/5.0 (Linux; Android 16; 24031PN0DC Build/BP2A.250605.031.A3; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/138.0.7204.180 Mobile Safari/537.36 XWEB/1380275 MMWEBSDK/20230806 MMWEBID/4102 wxworklocal/3.2.200 wwlocal/3.2.200 wxwork/4.0.0 appname/wxworklocal-customized wxworklocal-device-code/195ef5586d7d3c2808fcbea32d77c0d4 MicroMessenger/7.0.1 appScheme/wxworklocalcustomized Language/zh_CN ColorScheme/Light WXWorklocalClientType/Android Brand/xiaomi"
)
