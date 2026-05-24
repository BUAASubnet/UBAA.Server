package features

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/BUAASubnet/UBAA.Server/internal/storage"
	"github.com/BUAASubnet/UBAA.Server/internal/upstream"
)

func TestYgdkContextSelectionAndSummaryMapping(t *testing.T) {
	classifies := []map[string]any{
		{"classify_id": float64(2), "name": "其他"},
		{"classify_id": float64(3), "name": "阳光体育", "term_num": float64(30), "week_num": float64(3)},
	}
	classify := resolveYgdkSportsClassify(classifies)
	if ygdkInt(classify, "classify_id") != 3 {
		t.Fatalf("classify = %#v", classify)
	}
	items := []map[string]any{
		{"item_id": float64(1), "name": "步行", "sort": float64(2)},
		{"item_id": float64(2), "name": "跑步", "sort": float64(1)},
	}
	item := resolveYgdkDefaultItem(items)
	if ygdkInt(item, "item_id") != 2 {
		t.Fatalf("item = %#v", item)
	}
	summary := mapYgdkSummary(classify, map[string]any{"term_good_count_show": float64(8), "week_count": float64(2)}, map[string]any{"term_id": float64(9), "name": "春季"})
	if summary.TermCount != 8 || summary.TermID == nil || *summary.TermID != 9 || summary.WeekTarget == nil || *summary.WeekTarget != 3 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestYgdkTimeRangeParsingAndRecordMapping(t *testing.T) {
	startRaw := "2026-05-24 08:00"
	endRaw := "2026-05-24 09:00"
	start, end, err := resolveYgdkTimeRange(&startRaw, &endRaw, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if formatYgdkFormTime(start, end) != "2026-05-24 08:00-09:00" {
		t.Fatalf("form time = %q", formatYgdkFormTime(start, end))
	}
	itemMap := map[int]map[string]any{1: {"name": "跑步"}}
	record := mapYgdkRecord(map[string]any{
		"record_id":       float64(10),
		"item_id":         float64(1),
		"start_time":      float64(1779571200),
		"end_time":        float64(1779574800),
		"images":          `["a.jpg"]`,
		"isopen":          float64(1),
		"create_time_fmt": "2026-05-24",
	}, itemMap)
	if record.ItemName == nil || *record.ItemName != "跑步" || len(record.Images) != 1 || !record.IsOpen {
		t.Fatalf("record = %#v", record)
	}
}

func TestYgdkTransparentPNGGeneration(t *testing.T) {
	data, err := generateYgdkTransparentPNG()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 640 || img.Bounds().Dy() != 480 {
		t.Fatalf("bounds = %#v", img.Bounds())
	}
}

func TestYgdkCheckCountLogsInBeforeBuildingUserForm(t *testing.T) {
	var countUserID string
	var appServerURL string
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth":
			http.Redirect(w, r, appServerURL+"/callback?code=oauth-code", http.StatusFound)
		case "/api/Front/Clockin/User/campusAppLogin":
			if r.URL.Query().Get("code") != "oauth-code" {
				t.Fatalf("code = %q", r.URL.Query().Get("code"))
			}
			_, _ = w.Write([]byte(`{"code":1,"result":{"data":{"uid":123,"token":"tok%2B1"}}}`))
		case "/api/Front/Clockin/Clockin/getCount":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			countUserID = r.PostForm.Get("user_id")
			if r.PostForm.Get("uid") != "123" || r.PostForm.Get("token") != "tok+1" {
				t.Fatalf("post form = %#v", r.PostForm)
			}
			_, _ = w.Write([]byte(`{"code":1,"result":{"term_count":2}}`))
		default:
			t.Fatalf("unexpected YGDK request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer appServer.Close()
	appServerURL = appServer.URL

	db, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	oldTransport := http.DefaultTransport
	http.DefaultTransport = ygdkRewriteTransport{base: appServerURL, next: oldTransport}
	t.Cleanup(func() { http.DefaultTransport = oldTransport })

	service := NewService(upstream.NewClientFactory(db, upstream.NewURLRewriter()))
	client := &ygdkClient{username: "24182104", service: service}
	raw, err := client.CheckCount(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if countUserID != "123" {
		t.Fatalf("user_id = %q", countUserID)
	}
	if ygdkInt(raw, "term_count") != 2 {
		t.Fatalf("raw = %#v", raw)
	}
}

type ygdkRewriteTransport struct {
	base string
	next http.RoundTripper
}

func (t ygdkRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target, err := url.Parse(t.base)
	if err != nil {
		return nil, err
	}
	rewritten := req.Clone(req.Context())
	rewritten.URL.Scheme = target.Scheme
	rewritten.URL.Host = target.Host
	rewritten.Host = target.Host
	if req.URL.Host == "app.buaa.edu.cn" {
		rewritten.URL.Path = "/oauth"
		rewritten.URL.RawQuery = ""
	} else {
		rewritten.URL.Path = req.URL.EscapedPath()
		rewritten.URL.RawQuery = req.URL.RawQuery
	}
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}
	return next.RoundTrip(rewritten)
}
