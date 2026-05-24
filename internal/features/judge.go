package features

import (
	"context"
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
	"github.com/PuerkitoBio/goquery"
)

func (s *Service) GetJudgeAssignments(ctx context.Context, username string, includeExpired bool, skippedCourseIDs map[string]bool) (dto.JudgeAssignmentsResponse, error) {
	client := s.judgeClient(username)
	courses, err := client.Courses(ctx)
	if err != nil {
		return dto.JudgeAssignmentsResponse{}, err
	}
	assignments := []dto.JudgeAssignmentSummaryDto{}
	cutoffCourseIDs := map[string]bool{}
	for _, course := range courses {
		if !includeExpired && skippedCourseIDs[course.CourseID] {
			continue
		}
		rawAssignments, err := client.Assignments(ctx, course)
		if err != nil {
			return dto.JudgeAssignmentsResponse{}, err
		}
		for _, assignment := range rawAssignments {
			detail, err := client.AssignmentDetail(ctx, assignment.CourseID, assignment.CourseName, assignment.AssignmentID, assignment.Title)
			if err != nil {
				return dto.JudgeAssignmentsResponse{}, err
			}
			if judgeStartedBeforeCutoff(detail.StartTime) {
				cutoffCourseIDs[course.CourseID] = true
				if !includeExpired {
					break
				}
			}
			assignments = append(assignments, judgeDetailToSummary(detail))
		}
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
	cutoffs := make([]string, 0, len(cutoffCourseIDs))
	for courseID := range cutoffCourseIDs {
		cutoffs = append(cutoffs, courseID)
	}
	sort.Strings(cutoffs)
	return dto.JudgeAssignmentsResponse{Assignments: assignments, HistoricalCutoffCourseIDs: cutoffs}, nil
}

func (s *Service) GetJudgeAssignmentDetail(ctx context.Context, username, courseID, assignmentID string) (dto.JudgeAssignmentDetailDto, error) {
	details, err := s.GetJudgeAssignmentDetails(ctx, username, []dto.JudgeAssignmentDetailKeyDto{{CourseID: courseID, AssignmentID: assignmentID}})
	if err != nil {
		return dto.JudgeAssignmentDetailDto{}, err
	}
	if len(details.Details) == 0 {
		return dto.JudgeAssignmentDetailDto{}, ErrFeatureUpstream
	}
	return details.Details[0], nil
}

func (s *Service) GetJudgeAssignmentDetails(ctx context.Context, username string, keys []dto.JudgeAssignmentDetailKeyDto) (dto.JudgeAssignmentDetailsResponse, error) {
	client := s.judgeClient(username)
	courses, err := client.Courses(ctx)
	if err != nil {
		return dto.JudgeAssignmentDetailsResponse{}, err
	}
	courseMap := map[string]judgeCourseRaw{}
	for _, course := range courses {
		courseMap[course.CourseID] = course
	}
	seen := map[string]bool{}
	details := []dto.JudgeAssignmentDetailDto{}
	for _, key := range keys {
		if strings.TrimSpace(key.CourseID) == "" || strings.TrimSpace(key.AssignmentID) == "" {
			continue
		}
		seenKey := key.CourseID + "|" + key.AssignmentID
		if seen[seenKey] {
			continue
		}
		seen[seenKey] = true
		course, ok := courseMap[key.CourseID]
		if !ok {
			return dto.JudgeAssignmentDetailsResponse{}, ErrFeatureUpstream
		}
		assignments, err := client.Assignments(ctx, course)
		if err != nil {
			return dto.JudgeAssignmentDetailsResponse{}, err
		}
		for _, assignment := range assignments {
			if assignment.AssignmentID != key.AssignmentID {
				continue
			}
			detail, err := client.AssignmentDetail(ctx, assignment.CourseID, assignment.CourseName, assignment.AssignmentID, assignment.Title)
			if err != nil {
				return dto.JudgeAssignmentDetailsResponse{}, err
			}
			details = append(details, detail)
		}
	}
	return dto.JudgeAssignmentDetailsResponse{Details: details}, nil
}

func (s *Service) judgeClient(username string) *judgeClient {
	now := time.Now()
	if raw, ok := s.judgeClients.Load(username); ok {
		cached := raw.(*cachedJudgeClient)
		cached.lastAccessAt = now
		return cached.client
	}
	client := &judgeClient{username: username, service: s}
	cached := &cachedJudgeClient{client: client, lastAccessAt: now}
	actual, _ := s.judgeClients.LoadOrStore(username, cached)
	return actual.(*cachedJudgeClient).client
}

type judgeClient struct {
	username  string
	service   *Service
	activated bool
}

type judgeCourseRaw struct {
	CourseID   string
	CourseName string
}

type judgeAssignmentRaw struct {
	AssignmentID string
	CourseID     string
	CourseName   string
	Title        string
}

func (c *judgeClient) Courses(ctx context.Context) ([]judgeCourseRaw, error) {
	if err := c.ensureSession(ctx); err != nil {
		return nil, err
	}
	body, err := c.html(ctx, "https://judge.buaa.edu.cn/courselist.jsp?courseID=0")
	if err != nil {
		return nil, err
	}
	return parseJudgeCourses(body), nil
}

func (c *judgeClient) Assignments(ctx context.Context, course judgeCourseRaw) ([]judgeAssignmentRaw, error) {
	if err := c.ensureSession(ctx); err != nil {
		return nil, err
	}
	if _, err := c.html(ctx, "https://judge.buaa.edu.cn/courselist.jsp?courseID="+url.QueryEscape(course.CourseID)); err != nil {
		return nil, err
	}
	body, err := c.html(ctx, "https://judge.buaa.edu.cn/assignment/index.jsp")
	if err != nil {
		return nil, err
	}
	return parseJudgeAssignments(body, course), nil
}

func (c *judgeClient) AssignmentDetail(ctx context.Context, courseID, courseName, assignmentID, title string) (dto.JudgeAssignmentDetailDto, error) {
	if err := c.ensureSession(ctx); err != nil {
		return dto.JudgeAssignmentDetailDto{}, err
	}
	if _, err := c.html(ctx, "https://judge.buaa.edu.cn/courselist.jsp?courseID="+url.QueryEscape(courseID)); err != nil {
		return dto.JudgeAssignmentDetailDto{}, err
	}
	body, err := c.html(ctx, "https://judge.buaa.edu.cn/assignment/index.jsp?assignID="+url.QueryEscape(assignmentID))
	if err != nil {
		return dto.JudgeAssignmentDetailDto{}, err
	}
	return parseJudgeAssignmentDetail(body, courseID, courseName, assignmentID, title), nil
}

func (c *judgeClient) ensureSession(ctx context.Context) error {
	if c.activated {
		return nil
	}
	body, err := c.html(ctx, "https://sso.buaa.edu.cn/login?service=http%3A%2F%2Fjudge.buaa.edu.cn%2F")
	if err != nil {
		return err
	}
	if looksLikeJudgeSessionExpired("", body) {
		return ErrJudgeAuth
	}
	c.activated = true
	return nil
}

func (c *judgeClient) html(ctx context.Context, target string) (string, error) {
	sessionClient, err := c.service.clients.Get(c.username)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.service.clients.UpstreamURL(target), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", judgeAcceptHeader)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("User-Agent", judgeUserAgent)
	resp, err := sessionClient.Client.Do(req)
	if err != nil {
		return "", upstreamRequestError(err, ErrFeatureUpstream, "judge_timeout")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)
	if looksLikeJudgeSessionExpired(resp.Request.URL.String(), body) {
		c.activated = false
		return "", ErrJudgeAuth
	}
	if resp.StatusCode != http.StatusOK {
		return "", upstreamStatusError(resp.StatusCode, ErrFeatureUpstream, "judge_timeout")
	}
	return body, nil
}

func parseJudgeCourses(rawHTML string) []judgeCourseRaw {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return nil
	}
	courses := []judgeCourseRaw{}
	seen := map[string]bool{}
	doc.Find(`a[href*="courselist.jsp?courseID="]`).Each(func(_ int, anchor *goquery.Selection) {
		href, _ := anchor.Attr("href")
		match := regexp.MustCompile(`courseID=(\d+)`).FindStringSubmatch(href)
		if len(match) < 2 || match[1] == "0" || seen[match[1]] {
			return
		}
		name := cleanJudgeText(anchor.Text())
		if name == "" {
			return
		}
		seen[match[1]] = true
		courses = append(courses, judgeCourseRaw{CourseID: match[1], CourseName: name})
	})
	return courses
}

func parseJudgeAssignments(rawHTML string, course judgeCourseRaw) []judgeAssignmentRaw {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return nil
	}
	assignments := []judgeAssignmentRaw{}
	seen := map[string]bool{}
	doc.Find(`a[href*="assignID="]`).Each(func(_ int, anchor *goquery.Selection) {
		href, _ := anchor.Attr("href")
		if strings.Contains(href, "problemContent") || strings.Contains(href, "judgeDetails") {
			return
		}
		match := regexp.MustCompile(`assignID=(\d+)`).FindStringSubmatch(href)
		if len(match) < 2 || seen[match[1]] {
			return
		}
		title := cleanJudgeText(anchor.Text())
		if title == "" {
			return
		}
		seen[match[1]] = true
		assignments = append(assignments, judgeAssignmentRaw{
			AssignmentID: match[1],
			CourseID:     course.CourseID,
			CourseName:   course.CourseName,
			Title:        title,
		})
	})
	return assignments
}

func parseJudgeAssignmentDetail(rawHTML, courseID, courseName, assignmentID, title string) dto.JudgeAssignmentDetailDto {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	plainText := cleanJudgeText(rawHTML)
	var body *goquery.Selection
	if err == nil {
		plainText = cleanJudgeText(doc.Text())
		body = doc.Find("body")
	}
	startTime, dueTime := parseJudgeStartDue(plainText)
	maxScore := regexGroup(plainText, `作业满分[：:]\s*([\d.]+)`)
	totalProblems := 0
	if rawTotalProblems := regexGroup(plainText, `共\s*(\d+)\s*道`); rawTotalProblems != "" {
		totalProblems, _ = strconv.Atoi(rawTotalProblems)
	}
	explicitMyScore := regexGroup(plainText, `总分[：:]\s*([\d.]+)`)
	parsedProblems := parseJudgeProblems(body)
	problems := make([]dto.JudgeProblemDto, 0, len(parsedProblems))
	earnedScores := []float64{}
	for _, parsed := range parsedProblems {
		problems = append(problems, parsed.Problem)
		if parsed.EarnedScore != nil {
			earnedScores = append(earnedScores, *parsed.EarnedScore)
		}
	}
	submittedCount := 0
	if len(problems) > 0 {
		for _, problem := range problems {
			if problem.Status != "UNSUBMITTED" {
				submittedCount++
			}
		}
	} else {
		submittedCount = estimateJudgeSubmittedCount(plainText)
	}
	if totalProblems == 0 && len(problems) > 0 {
		totalProblems = len(problems)
	}
	myScore := explicitMyScore
	if myScore == "" && len(earnedScores) > 0 {
		sum := 0.0
		for _, score := range earnedScores {
			sum += score
		}
		myScore = formatJudgeScore(sum)
	}
	maxScore = normalizeJudgeScore(maxScore)
	myScore = normalizeJudgeScore(myScore)
	status := resolveJudgeStatus(totalProblems, submittedCount)
	return dto.JudgeAssignmentDetailDto{
		JudgeAssignmentSummaryDto: dto.JudgeAssignmentSummaryDto{
			CourseID:             courseID,
			CourseName:           courseName,
			AssignmentID:         assignmentID,
			Title:                title,
			StartTime:            stringPtrNonBlank(startTime),
			DueTime:              stringPtrNonBlank(dueTime),
			MaxScore:             stringPtrNonBlank(maxScore),
			MyScore:              stringPtrNonBlank(myScore),
			TotalProblems:        totalProblems,
			SubmittedCount:       submittedCount,
			SubmissionStatus:     status,
			SubmissionStatusText: judgeSubmissionStatusText(status, submittedCount, totalProblems, myScore, maxScore),
		},
		Problems:         problems,
		ContentPlainText: stringPtrNonBlank(plainText),
	}
}

type parsedJudgeProblem struct {
	Problem     dto.JudgeProblemDto
	EarnedScore *float64
}

func parseJudgeProblems(root *goquery.Selection) []parsedJudgeProblem {
	if root == nil {
		return nil
	}
	result := []parsedJudgeProblem{}
	root.Find("table").Each(func(_ int, table *goquery.Selection) {
		if table.ParentsFiltered("table").Length() > 0 {
			return
		}
		containers := table.ChildrenFiltered("tbody")
		if containers.Length() == 0 {
			containers = table
		}
		containers.ChildrenFiltered("tr").Each(func(_ int, row *goquery.Selection) {
			if parsed, ok := parseJudgeProblemFromRow(row); ok {
				result = append(result, parsed)
			}
		})
	})
	return result
}

func parseJudgeProblemFromRow(row *goquery.Selection) (parsedJudgeProblem, bool) {
	cells := []string{}
	row.ChildrenFiltered("th,td").Each(func(_ int, cell *goquery.Selection) {
		cells = append(cells, cleanJudgeText(cell.Text()))
	})
	if len(cells) >= 4 {
		maxScore, ok := parseJudgeNumber(cells[2])
		if !ok {
			return parsedJudgeProblem{}, false
		}
		statusText := strings.Join(cells[3:], " ")
		status, ok := detectJudgeProblemStatus(statusText)
		if !ok {
			return parsedJudgeProblem{}, false
		}
		earnedScore := parseJudgeEarnedScore(statusText)
		score := earnedScore
		if score == nil && status == "SUBMITTED" {
			score = &maxScore
		}
		return parsedJudgeProblem{
			Problem: dto.JudgeProblemDto{
				Name:       cells[1],
				Score:      formatJudgeScorePtr(score),
				MaxScore:   stringPtrNonBlank(formatJudgeScore(maxScore)),
				Status:     status,
				StatusText: judgeProblemStatusText(status),
			},
			EarnedScore: earnedScore,
		}, true
	}
	if len(cells) == 2 {
		status, ok := detectJudgeProblemStatus(cells[1])
		if !ok {
			return parsedJudgeProblem{}, false
		}
		earnedScore := parseJudgeEarnedScore(cells[1])
		index := strings.TrimSuffix(strings.TrimSpace(cells[0]), ".")
		name := "题目"
		if index != "" {
			name = "第" + index + "题"
		}
		return parsedJudgeProblem{
			Problem: dto.JudgeProblemDto{
				Name:       name,
				Score:      formatJudgeScorePtr(earnedScore),
				MaxScore:   formatJudgeScorePtr(earnedScore),
				Status:     status,
				StatusText: judgeProblemStatusText(status),
			},
			EarnedScore: earnedScore,
		}, true
	}
	return parsedJudgeProblem{}, false
}

func parseJudgeStartDue(text string) (string, string) {
	match := regexp.MustCompile(`作业时间[：:]\s*(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}(?::\d{2})?)\s*至\s*(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}(?::\d{2})?)`).FindStringSubmatch(text)
	if len(match) < 3 {
		return "", ""
	}
	return normalizeJudgeDateTime(match[1]), normalizeJudgeDateTime(match[2])
}

func detectJudgeProblemStatus(text string) (string, bool) {
	normalized := cleanJudgeText(text)
	for _, marker := range []string{"还未提交代码", "未提交文件", "未提交答案", "未作答", "未提交"} {
		if strings.Contains(normalized, marker) {
			return "UNSUBMITTED", true
		}
	}
	for _, marker := range []string{"初次提交时间", "首次提交时间", "最近一次提交时间", "最后一次提交时间", "最后一次修改时间", "已提交", "得分", "Accepted", "Accept"} {
		if strings.Contains(strings.ToLower(normalized), strings.ToLower(marker)) {
			return "SUBMITTED", true
		}
	}
	return "", false
}

func estimateJudgeSubmittedCount(text string) int {
	choiceEnd := len(text)
	for _, marker := range []string{"填空题", "编程题", "文件上传题"} {
		if index := strings.Index(text, marker); index >= 0 && index < choiceEnd {
			choiceEnd = index
		}
	}
	count := len(regexp.MustCompile(`得分[：:]\s*[\d.]+`).FindAllString(text[:choiceEnd], -1))
	if fillStart := strings.Index(text, "填空题"); fillStart >= 0 {
		next := len(text)
		for _, marker := range []string{"编程题", "文件上传题"} {
			if index := strings.Index(text[fillStart+len("填空题"):], marker); index >= 0 && fillStart+len("填空题")+index < next {
				next = fillStart + len("填空题") + index
			}
		}
		count += len(regexp.MustCompile(`得分[：:]\s*[\d.]+`).FindAllString(text[fillStart:next], -1))
	}
	if index := strings.Index(text, "编程题"); index >= 0 {
		count += len(regexp.MustCompile(`最后一次提交时间`).FindAllString(text[index:], -1))
	}
	if index := strings.Index(text, "文件上传题"); index >= 0 {
		count += len(regexp.MustCompile(`初次提交时间`).FindAllString(text[index:], -1))
	}
	return count
}

func resolveJudgeStatus(totalProblems, submittedCount int) string {
	switch {
	case totalProblems <= 0:
		return "UNKNOWN"
	case submittedCount <= 0:
		return "UNSUBMITTED"
	case submittedCount < totalProblems:
		return "PARTIAL"
	default:
		return "SUBMITTED"
	}
}

func judgeSubmissionStatusText(status string, submittedCount, totalProblems int, myScore, maxScore string) string {
	switch status {
	case "SUBMITTED":
		if myScore != "" && maxScore != "" {
			return "已完成 " + myScore + "/" + maxScore
		}
		return "已完成"
	case "PARTIAL":
		return fmt.Sprintf("进行中(%d/%d)", submittedCount, totalProblems)
	case "UNSUBMITTED":
		return "未提交"
	default:
		return "未知状态"
	}
}

func judgeProblemStatusText(status string) string {
	switch status {
	case "SUBMITTED":
		return "已提交"
	case "UNSUBMITTED":
		return "未提交"
	case "PARTIAL":
		return "部分提交"
	default:
		return "未知状态"
	}
}

func judgeDetailToSummary(detail dto.JudgeAssignmentDetailDto) dto.JudgeAssignmentSummaryDto {
	return detail.JudgeAssignmentSummaryDto
}

func judgeStartedBeforeCutoff(startTime *string) bool {
	if startTime == nil {
		return false
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", *startTime, time.Local)
	if err != nil {
		return false
	}
	return parsed.Before(time.Now().AddDate(0, -6, 0))
}

func parseJudgeNumber(value string) (float64, bool) {
	text := cleanJudgeText(value)
	if !regexp.MustCompile(`^\d+(?:\.\d+)?$`).MatchString(text) {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(text, 64)
	return parsed, err == nil
}

func parseJudgeEarnedScore(value string) *float64 {
	score := regexGroup(cleanJudgeText(value), `得分[：:]\s*([\d.]+)`)
	if score == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(score, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func normalizeJudgeDateTime(value string) string {
	if strings.Count(value, ":") == 1 {
		return value + ":00"
	}
	return value
}

func normalizeJudgeScore(value string) string {
	if value == "" {
		return ""
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return value
	}
	return formatJudgeScore(parsed)
}

func formatJudgeScore(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func formatJudgeScorePtr(value *float64) *string {
	if value == nil {
		return nil
	}
	text := formatJudgeScore(*value)
	return &text
}

func regexGroup(value, pattern string) string {
	match := regexp.MustCompile(pattern).FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func cleanJudgeText(value string) string {
	text := strings.ReplaceAll(value, "\u00a0", " ")
	return regexp.MustCompile(`\s+`).ReplaceAllString(strings.TrimSpace(text), " ")
}

func looksLikeJudgeSessionExpired(finalURL, body string) bool {
	if strings.Contains(strings.ToLower(finalURL), "sso.buaa.edu.cn/login") {
		return true
	}
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(strings.ToLower(trimmed), "<!doctype html") || strings.HasPrefix(strings.ToLower(trimmed), "<html") {
		return strings.Contains(body, `input name="execution"`) || strings.Contains(body, "统一身份认证")
	}
	return false
}

const (
	judgeAcceptHeader = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"
	judgeUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.3"
)
