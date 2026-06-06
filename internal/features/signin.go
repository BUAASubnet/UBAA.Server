package features

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/BUAASubnet/UBAA.Server/internal/dto"
	"github.com/BUAASubnet/UBAA.Server/internal/upstream"
)

type signinURLRewriter interface {
	UpstreamURL(raw string) string
}

type signinSessionClientFactory interface {
	signinURLRewriter
	NewNoRedirect(subject string) (*http.Client, error)
}

type signinClient struct {
	studentID string
	client    *http.Client
	upstream  signinSessionClientFactory
	mu        sync.Mutex
	userID    string
	sessionID string
	loginErr  string
}

func (c *signinClient) GetClasses(ctx context.Context, dateStr string) ([]dto.SigninClassDto, error) {
	return c.getClasses(ctx, dateStr, true)
}

func (c *signinClient) getClasses(ctx context.Context, dateStr string, allowRetry bool) ([]dto.SigninClassDto, error) {
	if !c.ensureLogin(ctx) {
		return []dto.SigninClassDto{}, nil
	}
	values := url.Values{}
	values.Set("id", c.userID)
	values.Set("dateStr", dateStr)
	req, err := c.newRequest(ctx, http.MethodGet, "https://iclass.buaa.edu.cn:8347/app/course/get_stu_course_sched.action?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("sessionId", c.sessionID)
	body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Status json.RawMessage `json:"STATUS"`
		Result []struct {
			ID             string          `json:"id"`
			CourseName     string          `json:"courseName"`
			ClassBeginTime string          `json:"classBeginTime"`
			ClassEndTime   string          `json:"classEndTime"`
			SignStatus     json.RawMessage `json:"signStatus"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return []dto.SigninClassDto{}, nil
	}
	if len(parsed.Status) > 0 && !signinStatusSuccess(parsed.Status) {
		if allowRetry {
			c.markUnauthenticated()
			if c.ensureLogin(ctx) {
				return c.getClasses(ctx, dateStr, false)
			}
		}
		return []dto.SigninClassDto{}, nil
	}
	classes := make([]dto.SigninClassDto, 0, len(parsed.Result))
	for _, item := range parsed.Result {
		classes = append(classes, dto.SigninClassDto{
			CourseID:       item.ID,
			CourseName:     item.CourseName,
			ClassBeginTime: item.ClassBeginTime,
			ClassEndTime:   item.ClassEndTime,
			SignStatus:     signinRawInt(item.SignStatus),
		})
	}
	return classes, nil
}

func (c *signinClient) SignIn(ctx context.Context, courseID string) (bool, string, error) {
	return c.signIn(ctx, courseID, true)
}

func (c *signinClient) signIn(ctx context.Context, courseID string, allowRetry bool) (bool, string, error) {
	if !c.ensureLogin(ctx) {
		message := c.loginErr
		c.loginErr = ""
		if strings.TrimSpace(message) == "" {
			message = "iclass 登录失败"
		}
		return false, message, nil
	}
	timestampReq, err := c.newRequest(ctx, http.MethodGet, "http://iclass.buaa.edu.cn:8081/app/common/get_timestamp.action", nil)
	if err != nil {
		return false, "", err
	}
	timestampBody, err := c.do(timestampReq)
	if err != nil {
		return false, "", err
	}
	var timestampResponse struct {
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(timestampBody, &timestampResponse); err != nil || timestampResponse.Timestamp == "" {
		return false, "获取服务器时间失败", nil
	}
	form := url.Values{}
	form.Set("id", c.userID)
	target := "http://iclass.buaa.edu.cn:8081/app/course/stu_scan_sign.action?courseSchedId=" +
		url.QueryEscape(courseID) + "&timestamp=" + url.QueryEscape(timestampResponse.Timestamp)
	req, err := c.newRequest(ctx, http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("sessionId", c.sessionID)
	body, err := c.do(req)
	if err != nil {
		return false, "", err
	}
	var parsed struct {
		Status json.RawMessage `json:"STATUS"`
		ErrMsg json.RawMessage `json:"ERRMSG"`
		Result *struct {
			StuSignStatus json.RawMessage `json:"stuSignStatus"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false, "签到失败，请稍后重试", nil
	}
	success := signinStatusSuccess(parsed.Status) && parsed.Result != nil && signinRawString(parsed.Result.StuSignStatus) == "1"
	rawMessage := signinRawString(parsed.ErrMsg)
	if !success && allowRetry && strings.Contains(rawMessage, "登录") {
		c.markUnauthenticated()
		if c.ensureLogin(ctx) {
			return c.signIn(ctx, courseID, false)
		}
	}
	return success, sanitizeSignInMessage(success, rawMessage), nil
}

func (c *signinClient) ensureLogin(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.userID != "" && c.sessionID != "" {
		return true
	}
	loginName := c.resolveLoginName(ctx)
	if loginName == "" {
		return false
	}
	values := url.Values{}
	values.Set("password", "")
	values.Set("phone", loginName)
	values.Set("userLevel", "1")
	values.Set("verificationType", "2")
	values.Set("verificationUrl", "")
	req, err := c.newRequest(ctx, http.MethodGet, "https://iclass.buaa.edu.cn:8347/app/user/login.action?"+values.Encode(), nil)
	if err != nil {
		return false
	}
	body, err := c.do(req)
	if err != nil {
		return false
	}
	var parsed struct {
		Status json.RawMessage `json:"STATUS"`
		ErrMsg json.RawMessage `json:"ERRMSG"`
		Result *struct {
			ID        string `json:"id"`
			SessionID string `json:"sessionId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || !signinStatusSuccess(parsed.Status) || parsed.Result == nil {
		if len(parsed.Status) > 0 && !signinStatusSuccess(parsed.Status) {
			c.loginErr = firstNonBlank(signinRawString(parsed.ErrMsg), "登录失败")
		}
		c.userID = ""
		c.sessionID = ""
		return false
	}
	c.userID = parsed.Result.ID
	c.sessionID = parsed.Result.SessionID
	c.loginErr = ""
	return c.userID != "" && c.sessionID != ""
}

func (c *signinClient) resolveLoginName(ctx context.Context) string {
	if c.upstream == nil {
		return c.studentID
	}
	client, err := c.upstream.NewNoRedirect(c.studentID)
	if err != nil {
		return ""
	}
	rawURL := signinMyCenterURL
	if loginName := extractSigninLoginName(rawURL); loginName != "" {
		return loginName
	}
	for range signinLoginRedirectLimit {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.rewriteURL(rawURL), nil)
		if err != nil {
			return ""
		}
		req.Header.Set("User-Agent", featureDefaultUserAgent)
		resp, err := client.Do(req)
		if err != nil {
			return ""
		}
		finalURL := upstream.FromWebVPNURL(resp.Request.URL.String())
		if loginName := extractSigninLoginName(finalURL); loginName != "" {
			_ = resp.Body.Close()
			return loginName
		}
		location := upstream.FromWebVPNURL(resp.Header.Get("Location"))
		if loginName := extractSigninLoginName(location); loginName != "" {
			_ = resp.Body.Close()
			return loginName
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 300 || resp.StatusCode > 399 || strings.TrimSpace(location) == "" {
			return ""
		}
		nextURL := resolveSigninRedirectURL(finalURL, location)
		if nextURL == "" {
			return ""
		}
		if loginName := extractSigninLoginName(nextURL); loginName != "" {
			return loginName
		}
		rawURL = nextURL
	}
	return ""
}

func (c *signinClient) markUnauthenticated() {
	c.userID = ""
	c.sessionID = ""
}

func (c *signinClient) newRequest(ctx context.Context, method, rawURL string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.rewriteURL(rawURL), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", featureDefaultUserAgent)
	return req, nil
}

func (c *signinClient) rewriteURL(rawURL string) string {
	if c.upstream != nil {
		return c.upstream.UpstreamURL(rawURL)
	}
	return upstream.ToWebVPNURL(rawURL)
}

func (c *signinClient) do(req *http.Request) ([]byte, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, ErrFeatureUpstream
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ErrFeatureUpstream
	}
	body, _ := io.ReadAll(resp.Body)
	return body, nil
}

func sanitizeSignInMessage(success bool, rawMessage string) string {
	if success {
		if strings.TrimSpace(rawMessage) != "" {
			return rawMessage
		}
		return "签到成功"
	}
	switch {
	case strings.Contains(rawMessage, "已签到"):
		return "您今天已经签到过了"
	case strings.Contains(rawMessage, "未开始"):
		return "当前还未到签到时间"
	case strings.Contains(rawMessage, "不是上课时间"):
		return "当前不是上课时间，无法签到"
	case strings.Contains(rawMessage, "已结束"):
		return "本次签到已结束"
	case strings.Contains(rawMessage, "范围"):
		return "当前不在可签到范围内"
	case strings.Contains(rawMessage, "用户不存在"):
		return "签到账号不存在，请联系管理员"
	case strings.Contains(rawMessage, "课程") && strings.Contains(rawMessage, "不存在"):
		return "未找到对应课程，请刷新后重试"
	default:
		return "签到失败，请稍后重试"
	}
}

func extractSigninLoginName(raw string) string {
	query := strings.SplitN(strings.TrimPrefix(strings.SplitN(raw, "#", 2)[0], "?"), "?", 2)
	if len(query) == 1 && !strings.Contains(raw, "?") {
		return ""
	}
	rawQuery := query[len(query)-1]
	for _, part := range strings.Split(rawQuery, "&") {
		key := part
		value := ""
		if before, after, ok := strings.Cut(part, "="); ok {
			key = before
			value = after
		}
		if !strings.EqualFold(key, "loginName") {
			continue
		}
		decoded := percentDecodeOnly(value)
		if strings.TrimSpace(decoded) != "" {
			return decoded
		}
	}
	return ""
}

func resolveSigninRedirectURL(currentURL, location string) string {
	target := strings.TrimSpace(location)
	if target == "" {
		return ""
	}
	if strings.HasPrefix(target, "/https-") || strings.HasPrefix(target, "/http-") ||
		strings.HasPrefix(target, "/wss-") || strings.HasPrefix(target, "/ws-") {
		return "https://d.buaa.edu.cn" + target
	}
	base, err := url.Parse(currentURL)
	if err != nil {
		return ""
	}
	next, err := url.Parse(target)
	if err != nil {
		return ""
	}
	return base.ResolveReference(next).String()
}

func percentDecodeOnly(value string) string {
	if !strings.Contains(value, "%") {
		return value
	}
	var builder strings.Builder
	builder.Grow(len(value))
	for i := 0; i < len(value); i++ {
		if value[i] == '%' && i+2 < len(value) {
			if decoded, ok := decodeHexByte(value[i+1], value[i+2]); ok {
				builder.WriteByte(decoded)
				i += 2
				continue
			}
		}
		builder.WriteByte(value[i])
	}
	return builder.String()
}

func signinStatusSuccess(raw json.RawMessage) bool {
	status := signinRawString(raw)
	return status == "0" || status == "200" || status == "success"
}

func signinRawString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err == nil {
		return number.String()
	}
	return ""
}

func signinRawInt(raw json.RawMessage) int {
	text := strings.TrimSpace(signinRawString(raw))
	if text == "" {
		return 0
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return 0
	}
	return parsed
}

func decodeHexByte(left, right byte) (byte, bool) {
	hi, ok := hexNibble(left)
	if !ok {
		return 0, false
	}
	lo, ok := hexNibble(right)
	if !ok {
		return 0, false
	}
	return hi<<4 | lo, true
}

func hexNibble(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

const (
	signinMyCenterURL        = "https://iclass.buaa.edu.cn:8346/?type=jumpMyCenter"
	signinLoginRedirectLimit = 8
	featureDefaultUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125 Safari/537.36"
)
