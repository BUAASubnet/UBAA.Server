package features

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSanitizeSignInMessage(t *testing.T) {
	cases := map[string]string{
		"您已签到":     "您今天已经签到过了",
		"签到未开始":    "当前还未到签到时间",
		"现在不是上课时间": "当前不是上课时间，无法签到",
		"签到已结束":    "本次签到已结束",
		"不在范围":     "当前不在可签到范围内",
		"用户不存在":    "签到账号不存在，请联系管理员",
		"课程不存在":    "未找到对应课程，请刷新后重试",
		"其他错误":     "签到失败，请稍后重试",
	}
	for raw, want := range cases {
		if got := sanitizeSignInMessage(false, raw); got != want {
			t.Fatalf("sanitizeSignInMessage(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := sanitizeSignInMessage(true, "OK"); got != "OK" {
		t.Fatalf("success message = %q", got)
	}
	if got := sanitizeSignInMessage(true, ""); got != "签到成功" {
		t.Fatalf("default success message = %q", got)
	}
}

func TestExtractSigninLoginName(t *testing.T) {
	loginName := "Rjc1QkJDMUMxNzVENkY0NkZCNzFDMEM5RjYwNzg4RDg="
	cases := map[string]string{
		"https://iclass.buaa.edu.cn:8346/?loginName=" + loginName + "&type=jumpMyCenter#/MyCenter": loginName,
		"https://iclass.buaa.edu.cn:8346/?type=jumpMyCenter&loginName=abc%2Bdef%2Fghi%3D":          "abc+def/ghi=",
		"https://iclass.buaa.edu.cn:8346/?loginname=" + loginName:                                  loginName,
		"/?loginName=" + loginName + "&type=jumpMyCenter#/MyCenter":                                loginName,
	}
	for raw, want := range cases {
		if got := extractSigninLoginName(raw); got != want {
			t.Fatalf("extractSigninLoginName(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := extractSigninLoginName("https://iclass.buaa.edu.cn:8346/?type=jumpMyCenter"); got != "" {
		t.Fatalf("missing loginName = %q", got)
	}
}

func TestResolveSigninRedirectURL(t *testing.T) {
	cases := map[string]string{
		"https://sso.buaa.edu.cn/login": "https://sso.buaa.edu.cn/login",
		"/?type=jumpMyCenter#/MyCenter": "https://iclass.buaa.edu.cn:8346/?type=jumpMyCenter#/MyCenter",
		"cas/final":                     "https://iclass.buaa.edu.cn:8346/cas/final",
		"?loginName=abc%2Bdef%3D":       "https://iclass.buaa.edu.cn:8346/cas-login?loginName=abc%2Bdef%3D",
		"#/MyCenter":                    "https://iclass.buaa.edu.cn:8346/cas-login?ticket=ST-test#/MyCenter",
		"/https-8346/encrypted/path":    "https://d.buaa.edu.cn/https-8346/encrypted/path",
	}
	for location, want := range cases {
		if got := resolveSigninRedirectURL("https://iclass.buaa.edu.cn:8346/cas-login?ticket=ST-test", location); got != want {
			t.Fatalf("resolveSigninRedirectURL(%q) = %q, want %q", location, got, want)
		}
	}
}

func TestSigninClientFollowsSSORedirectsAndLogsInWithLoginName(t *testing.T) {
	loginName := "abc+def/ghi="
	loginNameParam := url.QueryEscape(loginName)
	var myCenterRequests int
	var appLoginPhones []string
	var appServerURL string

	ssoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/" && r.URL.Query().Get("type") == "jumpMyCenter":
			myCenterRequests++
			if myCenterRequests == 1 {
				http.Redirect(w, r, "https://sso.buaa.edu.cn/login?service=https%3A%2F%2Ficlass.buaa.edu.cn%3A8346%2F%3Ftype%3DjumpMyCenter", http.StatusFound)
				return
			}
			http.Redirect(w, r, "https://iclass.buaa.edu.cn:8346/?loginName="+loginNameParam+"&type=jumpMyCenter#/MyCenter", http.StatusFound)
		case r.URL.Path == "/login":
			http.Redirect(w, r, "https://iclass.buaa.edu.cn:8346/cas-login?ticket=ST-test", http.StatusFound)
		case r.URL.Path == "/cas-login":
			http.Redirect(w, r, "/?type=jumpMyCenter#/MyCenter", http.StatusFound)
		default:
			t.Fatalf("unexpected SSO request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer ssoServer.Close()

	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/user/login.action":
			appLoginPhones = append(appLoginPhones, r.URL.Query().Get("phone"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"STATUS": 0,
				"result": map[string]any{"id": "user-1", "sessionId": "session-1"},
			})
		case "/app/course/get_stu_course_sched.action":
			if r.URL.Query().Get("id") != "user-1" || r.URL.Query().Get("dateStr") != "20260524" {
				t.Fatalf("unexpected class query: %s", r.URL.RawQuery)
			}
			if r.Header.Get("sessionId") != "session-1" {
				t.Fatalf("sessionId = %q", r.Header.Get("sessionId"))
			}
			_, _ = io.WriteString(w, `{"STATUS":0,"result":[{"id":"course-1","courseName":"软件工程","classBeginTime":"08:00","classEndTime":"09:40","signStatus":0}]}`)
		default:
			t.Fatalf("unexpected app request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer appServer.Close()
	appServerURL = appServer.URL

	rewriter := signinTestUpstream{rewrite: func(raw string) string {
		switch {
		case strings.HasPrefix(raw, signinMyCenterURL):
			return ssoServer.URL + "/?type=jumpMyCenter"
		case strings.HasPrefix(raw, "https://sso.buaa.edu.cn/login"):
			return ssoServer.URL + strings.TrimPrefix(raw, "https://sso.buaa.edu.cn")
		case strings.HasPrefix(raw, "https://iclass.buaa.edu.cn:8346/cas-login"):
			return ssoServer.URL + "/cas-login?" + strings.SplitN(raw, "?", 2)[1]
		case strings.HasPrefix(raw, "https://iclass.buaa.edu.cn:8346/"):
			return ssoServer.URL + strings.TrimPrefix(raw, "https://iclass.buaa.edu.cn:8346")
		case strings.HasPrefix(raw, "https://iclass.buaa.edu.cn:8347"):
			return appServerURL + strings.TrimPrefix(raw, "https://iclass.buaa.edu.cn:8347")
		default:
			return raw
		}
	}}
	client := &signinClient{studentID: "24182104", client: appServer.Client(), upstream: rewriter}
	classes, err := client.GetClasses(context.Background(), "20260524")
	if err != nil {
		t.Fatal(err)
	}
	if len(appLoginPhones) != 1 || appLoginPhones[0] != loginName {
		t.Fatalf("appLoginPhones = %#v", appLoginPhones)
	}
	if myCenterRequests != 2 {
		t.Fatalf("myCenterRequests = %d", myCenterRequests)
	}
	if len(classes) != 1 || classes[0].CourseID != "course-1" {
		t.Fatalf("classes = %#v", classes)
	}
}

func TestSigninClientAcceptsStringStatusesAndRetriesClassLoad(t *testing.T) {
	loginName := "iclass-login-name"
	var classSessions []string
	var appLoginPhones []string
	var serverURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jump":
			http.Redirect(w, r, "https://iclass.buaa.edu.cn:8346/?loginName="+url.QueryEscape(loginName)+"&type=jumpMyCenter#/MyCenter", http.StatusFound)
		case "/app/user/login.action":
			appLoginPhones = append(appLoginPhones, r.URL.Query().Get("phone"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"STATUS": "success",
				"result": map[string]any{"id": "new-user", "sessionId": "new-session"},
			})
		case "/app/course/get_stu_course_sched.action":
			classSessions = append(classSessions, r.Header.Get("sessionId"))
			if len(classSessions) == 1 {
				_, _ = io.WriteString(w, `{"STATUS":"401","ERRMSG":"登录已失效"}`)
				return
			}
			_, _ = io.WriteString(w, `{"STATUS":"200","result":[{"id":"course-1","courseName":"软件工程","classBeginTime":"08:00","classEndTime":"09:40","signStatus":"1"}]}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	serverURL = server.URL

	client := &signinClient{
		studentID: "24182104",
		client:    server.Client(),
		upstream:  signinTestRewriter(serverURL),
		userID:    "old-user",
		sessionID: "old-session",
	}
	classes, err := client.GetClasses(context.Background(), "20260524")
	if err != nil {
		t.Fatal(err)
	}
	if len(appLoginPhones) != 1 || appLoginPhones[0] != loginName {
		t.Fatalf("appLoginPhones = %#v", appLoginPhones)
	}
	if strings.Join(classSessions, ",") != "old-session,new-session" {
		t.Fatalf("classSessions = %#v", classSessions)
	}
	if len(classes) != 1 || classes[0].SignStatus != 1 {
		t.Fatalf("classes = %#v", classes)
	}
}

func TestSigninClientSendsSessionHeaderAndRetriesSignin(t *testing.T) {
	loginName := "iclass-login-name"
	var signinSessions []string
	var appLoginPhones []string
	var serverURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jump":
			http.Redirect(w, r, "https://iclass.buaa.edu.cn:8346/?loginName="+url.QueryEscape(loginName)+"&type=jumpMyCenter#/MyCenter", http.StatusFound)
		case "/app/user/login.action":
			appLoginPhones = append(appLoginPhones, r.URL.Query().Get("phone"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"STATUS": 0,
				"result": map[string]any{"id": "new-user", "sessionId": "new-session"},
			})
		case "/app/common/get_timestamp.action":
			_, _ = io.WriteString(w, `{"timestamp":"20260524120000"}`)
		case "/app/course/stu_scan_sign.action":
			signinSessions = append(signinSessions, r.Header.Get("sessionId"))
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("courseSchedId") != "course-1" || r.Form.Get("timestamp") != "20260524120000" {
				t.Fatalf("unexpected signin form/query: %s %#v", r.URL.RawQuery, r.Form)
			}
			if len(signinSessions) == 1 {
				if r.Form.Get("id") != "old-user" {
					t.Fatalf("first signin id = %q", r.Form.Get("id"))
				}
				_, _ = io.WriteString(w, `{"STATUS":1,"ERRMSG":"登录已失效"}`)
				return
			}
			if r.Form.Get("id") != "new-user" {
				t.Fatalf("second signin id = %q", r.Form.Get("id"))
			}
			_, _ = io.WriteString(w, `{"STATUS":"0","ERRMSG":"OK","result":{"stuSignStatus":"1"}}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	serverURL = server.URL

	client := &signinClient{
		studentID: "24182104",
		client:    server.Client(),
		upstream:  signinTestRewriter(serverURL),
		userID:    "old-user",
		sessionID: "old-session",
	}
	success, message, err := client.SignIn(context.Background(), "course-1")
	if err != nil {
		t.Fatal(err)
	}
	if !success || message != "OK" {
		t.Fatalf("SignIn = %v, %q", success, message)
	}
	if len(appLoginPhones) != 1 || appLoginPhones[0] != loginName {
		t.Fatalf("appLoginPhones = %#v", appLoginPhones)
	}
	if strings.Join(signinSessions, ",") != "old-session,new-session" {
		t.Fatalf("signinSessions = %#v", signinSessions)
	}
}

type signinTestUpstream struct {
	rewrite func(string) string
}

func (u signinTestUpstream) UpstreamURL(raw string) string {
	return u.rewrite(raw)
}

func (u signinTestUpstream) NewNoRedirect(_ string) (*http.Client, error) {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

func signinTestRewriter(serverURL string) signinTestUpstream {
	return signinTestUpstream{rewrite: func(raw string) string {
		switch {
		case strings.HasPrefix(raw, signinMyCenterURL):
			return serverURL + "/jump"
		case strings.HasPrefix(raw, "https://iclass.buaa.edu.cn:8346/"):
			return serverURL + strings.TrimPrefix(raw, "https://iclass.buaa.edu.cn:8346")
		case strings.HasPrefix(raw, "https://iclass.buaa.edu.cn:8347"):
			return serverURL + strings.TrimPrefix(raw, "https://iclass.buaa.edu.cn:8347")
		case strings.HasPrefix(raw, "http://iclass.buaa.edu.cn:8081"):
			return serverURL + strings.TrimPrefix(raw, "http://iclass.buaa.edu.cn:8081")
		default:
			return raw
		}
	}}
}
