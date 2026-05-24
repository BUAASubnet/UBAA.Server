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
	"strings"
	"time"

	"github.com/BUAASubnet/UBAA.Server/internal/dto"
	"golang.org/x/net/html"
)

func (s *Service) GetSpocAssignments(ctx context.Context, username string) (dto.SpocAssignmentsResponse, error) {
	client := s.spocClient(username)
	term, err := client.CurrentTerm(ctx)
	if err != nil {
		return dto.SpocAssignmentsResponse{}, err
	}
	termCode := strings.TrimSpace(term.Mrxq)
	if termCode == "" {
		return dto.SpocAssignmentsResponse{}, ErrFeatureUpstream
	}
	courses, _ := client.Courses(ctx, termCode)
	courseMap := map[string]spocCourseRaw{}
	for _, course := range courses {
		courseMap[course.Kcid] = course
	}
	rawAssignments, err := client.AllAssignments(ctx, termCode)
	if err != nil {
		return dto.SpocAssignmentsResponse{}, err
	}
	assignments := make([]dto.SpocAssignmentSummaryDto, 0, len(rawAssignments))
	for _, assignment := range rawAssignments {
		course := courseMap[assignment.Sskcid]
		status := spocSubmissionStatus(assignment.Tjzt, strings.TrimSpace(assignment.Tjzt) != "")
		courseName := assignment.Kcmc
		if strings.TrimSpace(courseName) == "" {
			courseName = course.Kcmc
		}
		assignments = append(assignments, dto.SpocAssignmentSummaryDto{
			AssignmentID:         assignment.Zyid,
			CourseID:             assignment.Sskcid,
			CourseName:           courseName,
			TeacherName:          stringPtrNonBlank(course.Skjs),
			Title:                assignment.Zymc,
			StartTime:            stringPtrNonBlank(normalizeSpocDateTime(assignment.Zykssj)),
			DueTime:              stringPtrNonBlank(normalizeSpocDateTime(assignment.Zyjzsj)),
			Score:                stringPtrNonBlank(normalizeSpocScore(assignment.Mf)),
			SubmissionStatus:     status,
			SubmissionStatusText: spocSubmissionStatusText(status, assignment.Tjzt),
		})
	}
	sort.SliceStable(assignments, func(i, j int) bool {
		leftDue := "9999-99-99 99:99:99"
		rightDue := "9999-99-99 99:99:99"
		if assignments[i].DueTime != nil {
			leftDue = *assignments[i].DueTime
		}
		if assignments[j].DueTime != nil {
			rightDue = *assignments[j].DueTime
		}
		if leftDue != rightDue {
			return leftDue < rightDue
		}
		if assignments[i].CourseName != assignments[j].CourseName {
			return assignments[i].CourseName < assignments[j].CourseName
		}
		return assignments[i].Title < assignments[j].Title
	})
	return dto.SpocAssignmentsResponse{
		TermCode:    termCode,
		TermName:    stringPtrNonBlank(term.Dqxq),
		Assignments: assignments,
	}, nil
}

func (s *Service) GetSpocAssignmentDetail(ctx context.Context, username, assignmentID string) (dto.SpocAssignmentDetailDto, error) {
	response, err := s.GetSpocAssignments(ctx, username)
	if err != nil {
		return dto.SpocAssignmentDetailDto{}, err
	}
	var summary *dto.SpocAssignmentSummaryDto
	for i := range response.Assignments {
		if response.Assignments[i].AssignmentID == assignmentID {
			summary = &response.Assignments[i]
			break
		}
	}
	if summary == nil {
		return dto.SpocAssignmentDetailDto{}, ErrFeatureUpstream
	}
	client := s.spocClient(username)
	detail, err := client.AssignmentDetail(ctx, assignmentID)
	if err != nil {
		return dto.SpocAssignmentDetailDto{}, err
	}
	submission, _ := client.Submission(ctx, assignmentID)
	hasSubmission := submission != nil
	rawStatus := ""
	submittedAt := (*string)(nil)
	if submission != nil {
		rawStatus = submission.Tjzt
		submittedAt = stringPtrNonBlank(normalizeSpocDateTime(submission.Tjsj))
	}
	status := spocSubmissionStatus(rawStatus, hasSubmission)
	score := summary.Score
	if normalized := normalizeSpocScore(detail.Zyfs); normalized != "" {
		score = &normalized
	}
	startTime := summary.StartTime
	if normalized := normalizeSpocDateTime(detail.Zykssj); normalized != "" {
		startTime = &normalized
	}
	dueTime := summary.DueTime
	if normalized := normalizeSpocDateTime(detail.Zyjzsj); normalized != "" {
		dueTime = &normalized
	}
	return dto.SpocAssignmentDetailDto{
		SpocAssignmentSummaryDto: dto.SpocAssignmentSummaryDto{
			AssignmentID:         summary.AssignmentID,
			CourseID:             summary.CourseID,
			CourseName:           summary.CourseName,
			TeacherName:          summary.TeacherName,
			Title:                summary.Title,
			StartTime:            startTime,
			DueTime:              dueTime,
			Score:                score,
			SubmissionStatus:     status,
			SubmissionStatusText: spocSubmissionStatusText(status, rawStatus),
		},
		ContentPlainText: htmlPlainText(detail.Zynr),
		ContentHTML:      stringPtrNonBlank(detail.Zynr),
		SubmittedAt:      submittedAt,
	}, nil
}

func (s *Service) spocClient(username string) *spocClient {
	now := time.Now()
	if raw, ok := s.spocClients.Load(username); ok {
		cached := raw.(*cachedSpocClient)
		cached.lastAccessAt = now
		return cached.client
	}
	client := &spocClient{username: username, service: s}
	cached := &cachedSpocClient{client: client, lastAccessAt: now}
	actual, _ := s.spocClients.LoadOrStore(username, cached)
	return actual.(*cachedSpocClient).client
}

type spocClient struct {
	username string
	service  *Service
	token    string
	roleCode string
}

type spocCurrentTermContent struct {
	Dqxq string `json:"dqxq"`
	Mrxq string `json:"mrxq"`
}

type spocCourseRaw struct {
	Kcid string `json:"kcid"`
	Kcmc string `json:"kcmc"`
	Skjs string `json:"skjs"`
}

type spocAssignmentsPageContent struct {
	Total       int                      `json:"total"`
	List        []spocPagedAssignmentRaw `json:"list"`
	PageNum     int                      `json:"pageNum"`
	PageSize    int                      `json:"pageSize"`
	Pages       int                      `json:"pages"`
	HasNextPage bool                     `json:"hasNextPage"`
}

type spocPagedAssignmentRaw struct {
	Zyid   string `json:"zyid"`
	Tjzt   string `json:"tjzt"`
	Zyjzsj string `json:"zyjzsj"`
	Zymc   string `json:"zymc"`
	Zykssj string `json:"zykssj"`
	Sskcid string `json:"sskcid"`
	Xnxq   string `json:"xnxq"`
	Mf     string `json:"mf"`
	Kcmc   string `json:"kcmc"`
}

type spocAssignmentDetailRaw struct {
	ID     string `json:"id"`
	Zymc   string `json:"zymc"`
	Zynr   string `json:"zynr"`
	Zykssj string `json:"zykssj"`
	Zyjzsj string `json:"zyjzsj"`
	Zyfs   string `json:"zyfs"`
	Sskcid string `json:"sskcid"`
}

type spocSubmissionRaw struct {
	Tjzt string `json:"tjzt"`
	Tjsj string `json:"tjsj"`
}

type spocEnvelope[T any] struct {
	Code    int     `json:"code"`
	Msg     *string `json:"msg"`
	MsgEn   *string `json:"msg_en"`
	Content *T      `json:"content"`
}

func (c *spocClient) CurrentTerm(ctx context.Context) (spocCurrentTermContent, error) {
	var result spocCurrentTermContent
	err := c.postEnvelope(ctx, "https://spoc.buaa.edu.cn/spocnewht/inco/ht/queryOne", map[string]string{"param": currentTermParam}, &result)
	return result, err
}

func (c *spocClient) Courses(ctx context.Context, termCode string) ([]spocCourseRaw, error) {
	var result []spocCourseRaw
	query := url.Values{}
	query.Set("kcmc", "")
	query.Set("xnxq", termCode)
	err := c.getEnvelope(ctx, "https://spoc.buaa.edu.cn/spocnewht/jxkj/queryKclb?"+query.Encode(), &result)
	return result, err
}

func (c *spocClient) AllAssignments(ctx context.Context, termCode string) ([]spocPagedAssignmentRaw, error) {
	assignments := []spocPagedAssignmentRaw{}
	for pageNum := 1; ; pageNum++ {
		page, err := c.AssignmentsPage(ctx, termCode, pageNum, 15)
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, page.List...)
		if !page.HasNextPage || pageNum >= page.Pages || len(page.List) == 0 {
			break
		}
	}
	return assignments, nil
}

func (c *spocClient) AssignmentsPage(ctx context.Context, termCode string, pageNum, pageSize int) (spocAssignmentsPageContent, error) {
	payload := map[string]any{
		"pageSize": pageSize,
		"pageNum":  pageNum,
		"sqlid":    assignmentsPageSQLID,
		"xnxq":     termCode,
		"kcid":     "",
		"yzwz":     "",
	}
	raw, _ := json.Marshal(payload)
	var result spocAssignmentsPageContent
	err := c.postEnvelope(ctx, "https://spoc.buaa.edu.cn/spocnewht/inco/ht/queryListByPage", map[string]string{"param": encryptSpocParam(string(raw))}, &result)
	return result, err
}

func (c *spocClient) AssignmentDetail(ctx context.Context, assignmentID string) (spocAssignmentDetailRaw, error) {
	var result spocAssignmentDetailRaw
	query := url.Values{}
	query.Set("id", assignmentID)
	err := c.getEnvelope(ctx, "https://spoc.buaa.edu.cn/spocnewht/kczy/queryKczyInfoByid?"+query.Encode(), &result)
	return result, err
}

func (c *spocClient) Submission(ctx context.Context, assignmentID string) (*spocSubmissionRaw, error) {
	var result spocSubmissionRaw
	query := url.Values{}
	query.Set("kczyid", assignmentID)
	err := c.getEnvelope(ctx, "https://spoc.buaa.edu.cn/spocnewht/kczy/queryXsSubmitKczyInfo?"+query.Encode(), &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *spocClient) ensureLogin(ctx context.Context, force bool) error {
	if !force && c.token != "" && c.roleCode != "" {
		return nil
	}
	token, err := c.fetchLoginToken(ctx)
	if err != nil {
		return err
	}
	roleCode, err := c.casLogin(ctx, token)
	if err != nil {
		return err
	}
	c.token = token
	c.roleCode = roleCode
	return nil
}

func (c *spocClient) fetchLoginToken(ctx context.Context) (string, error) {
	client, err := c.service.clients.NewNoRedirect(c.username)
	if err != nil {
		return "", err
	}
	currentURL := c.service.clients.UpstreamURL("https://spoc.buaa.edu.cn/spocnewht/cas")
	for range 8 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentURL, nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", upstreamRequestError(err, ErrFeatureUpstream, "spoc_timeout")
		}
		if token := extractSpocLoginToken(resp.Request.URL.String()); token != "" {
			_ = resp.Body.Close()
			return token, nil
		}
		location := resp.Header.Get("Location")
		_ = resp.Body.Close()
		if token := extractSpocLoginToken(location); token != "" {
			return token, nil
		}
		if location == "" {
			return "", ErrFeatureUpstream
		}
		currentURL = resolveFeatureLocation(resp.Request.URL, location)
	}
	return "", ErrFeatureUpstream
}

func (c *spocClient) casLogin(ctx context.Context, token string) (string, error) {
	var result struct {
		Jsdm     any `json:"jsdm"`
		RoleCode any `json:"rolecode"`
		JsdmList any `json:"jsdmList"`
	}
	err := c.postEnvelopeWithToken(ctx, "https://spoc.buaa.edu.cn/spocnewht/sys/casLogin", map[string]string{"token": token}, token, "", &result)
	if err != nil {
		return "", err
	}
	roleCode := firstString(result.Jsdm)
	if roleCode == "" {
		roleCode = firstString(result.RoleCode)
	}
	if roleCode == "" {
		roleCode = firstString(result.JsdmList)
	}
	if roleCode == "" {
		return "", ErrSpocAuth
	}
	return roleCode, nil
}

func (c *spocClient) getEnvelope(ctx context.Context, target string, out any) error {
	if err := c.ensureLogin(ctx, false); err != nil {
		return err
	}
	err := c.getEnvelopeOnce(ctx, target, out)
	if err == nil {
		return nil
	}
	if !errorsIsUnauth(err) {
		return err
	}
	if err := c.ensureLogin(ctx, true); err != nil {
		return err
	}
	return c.getEnvelopeOnce(ctx, target, out)
}

func (c *spocClient) postEnvelope(ctx context.Context, target string, body any, out any) error {
	if err := c.ensureLogin(ctx, false); err != nil {
		return err
	}
	err := c.postEnvelopeWithToken(ctx, target, body, c.token, c.roleCode, out)
	if err == nil {
		return nil
	}
	if !errorsIsUnauth(err) {
		return err
	}
	if err := c.ensureLogin(ctx, true); err != nil {
		return err
	}
	return c.postEnvelopeWithToken(ctx, target, body, c.token, c.roleCode, out)
}

func (c *spocClient) getEnvelopeOnce(ctx context.Context, target string, out any) error {
	client, err := c.service.clients.Get(c.username)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.service.clients.UpstreamURL(target), nil)
	if err != nil {
		return err
	}
	c.applySpocHeaders(req, c.token, c.roleCode)
	resp, err := client.Client.Do(req)
	if err != nil {
		return upstreamRequestError(err, ErrFeatureUpstream, "spoc_timeout")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return decodeSpocEnvelope(body, out)
}

func (c *spocClient) postEnvelopeWithToken(ctx context.Context, target string, payload any, token, roleCode string, out any) error {
	client, err := c.service.clients.Get(c.username)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.service.clients.UpstreamURL(target), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	c.applySpocHeaders(req, token, roleCode)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Client.Do(req)
	if err != nil {
		return upstreamRequestError(err, ErrFeatureUpstream, "spoc_timeout")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return decodeSpocEnvelope(body, out)
}

func (c *spocClient) applySpocHeaders(req *http.Request, token, roleCode string) {
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if token != "" {
		req.Header.Set("Token", "Inco-"+token)
	}
	if roleCode != "" {
		req.Header.Set("RoleCode", roleCode)
	}
}

func decodeSpocEnvelope(raw []byte, out any) error {
	if isSessionExpired(&http.Response{StatusCode: http.StatusOK, Request: &http.Request{URL: &url.URL{}}}, raw) {
		return ErrSpocAuth
	}
	var envelope struct {
		Code    int             `json:"code"`
		Msg     *string         `json:"msg"`
		MsgEn   *string         `json:"msg_en"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		if looksLikeSpocAuthFailure("", string(raw)) {
			return ErrSpocAuth
		}
		return ErrFeatureUpstream
	}
	if envelope.Code != 200 {
		message := ""
		if envelope.Msg != nil {
			message = *envelope.Msg
		}
		if looksLikeSpocAuthFailure(message, string(raw)) {
			return ErrSpocAuth
		}
		return ErrFeatureUpstream
	}
	if len(envelope.Content) == 0 || string(envelope.Content) == "null" {
		return nil
	}
	if err := json.Unmarshal(envelope.Content, out); err != nil {
		return ErrFeatureUpstream
	}
	return nil
}

func encryptSpocParam(plainText string) string {
	key := []byte("inco12345678ocni")
	iv := []byte("ocni12345678inco")
	block, err := aes.NewCipher(key)
	if err != nil {
		return ""
	}
	plain := []byte(plainText)
	if remainder := len(plain) % aes.BlockSize; remainder != 0 {
		plain = append(plain, make([]byte, aes.BlockSize-remainder)...)
	}
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(encrypted, plain)
	return base64.StdEncoding.EncodeToString(encrypted)
}

func extractSpocLoginToken(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || !strings.Contains(parsed.Path, "/spocnew/cas") {
		return ""
	}
	return parsed.Query().Get("token")
}

func spocSubmissionStatus(rawStatus string, hasContent bool) string {
	switch strings.TrimSpace(rawStatus) {
	case "1", "已做", "已提交":
		return "SUBMITTED"
	case "0", "未做", "未提交":
		return "UNSUBMITTED"
	default:
		if !hasContent {
			return "UNSUBMITTED"
		}
		return "UNKNOWN"
	}
}

func spocSubmissionStatusText(status, rawStatus string) string {
	switch status {
	case "SUBMITTED":
		return "已提交"
	case "UNSUBMITTED":
		return "未提交"
	default:
		if strings.TrimSpace(rawStatus) != "" {
			return "未知状态(" + rawStatus + ")"
		}
		return "未知状态"
	}
}

func normalizeSpocScore(rawScore string) string {
	normalized := strings.TrimSpace(rawScore)
	if normalized == "" {
		return ""
	}
	match := regexp.MustCompile(`-?\d+(?:\.\d+)?`).FindString(normalized)
	if match != "" {
		return match
	}
	return normalized
}

func normalizeSpocDateTime(rawValue string) string {
	normalized := strings.TrimSpace(rawValue)
	if normalized == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339, normalized); err == nil {
		return parsed.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02 15:04:05")
	}
	if parsed, err := time.Parse("2006-01-02T15:04:05.000-07:00", normalized); err == nil {
		return parsed.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02 15:04:05")
	}
	return strings.Split(strings.ReplaceAll(normalized, "T", " "), ".")[0]
}

func htmlPlainText(rawHTML string) *string {
	if strings.TrimSpace(rawHTML) == "" {
		return nil
	}
	node, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		text := strings.TrimSpace(rawHTML)
		return &text
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			builder.WriteString(n.Data)
			builder.WriteString(" ")
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	text := regexp.MustCompile(`\s+`).ReplaceAllString(strings.TrimSpace(builder.String()), " ")
	if text == "" {
		return nil
	}
	return &text
}

func firstString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		for _, item := range typed {
			if text := firstString(item); text != "" {
				return text
			}
		}
	case []string:
		for _, item := range typed {
			if strings.TrimSpace(item) != "" {
				return strings.TrimSpace(item)
			}
		}
	}
	return ""
}

func looksLikeSpocAuthFailure(message, body string) bool {
	text := strings.ToLower(message + " " + body)
	return strings.Contains(text, "登录") ||
		strings.Contains(text, "token") ||
		strings.Contains(text, "未认证") ||
		strings.Contains(text, "未登录") ||
		strings.Contains(text, "权限")
}

func resolveFeatureLocation(base *url.URL, location string) string {
	parsed, err := url.Parse(location)
	if err != nil {
		return location
	}
	return base.ResolveReference(parsed).String()
}

func stringPtrNonBlank(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func errorsIsUnauth(err error) bool {
	return errors.Is(err, ErrFeatureUnauthenticated) ||
		errors.Is(err, ErrSpocAuth) ||
		(err != nil && strings.Contains(fmt.Sprint(err), ErrFeatureUnauthenticated.Error())) ||
		(err != nil && strings.Contains(fmt.Sprint(err), ErrSpocAuth.Error()))
}

const (
	currentTermParam     = "YHrxtTavu6raCwC0/qdgYffB9evWHBkTng/XS4W6j3f/TPo02iEPSoegscDTRNzIPRG49o3RHl4JiFCXAiBkkA=="
	assignmentsPageSQLID = "1713252980496efac7d5d9985e81693116d3e8a52ebf2b"
)
