package features

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestBykcCourseStatusAndCampusMappingMatchKtorBehavior(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.Local)
	course := map[string]any{
		"id":                    float64(1),
		"courseName":            " 博雅课 ",
		"courseStartDate":       "2026-06-01 12:00:00",
		"courseSelectStartDate": "2026-05-01 00:00:00",
		"courseSelectEndDate":   "2026-05-30 00:00:00",
		"courseMaxCount":        float64(10),
		"courseCurrentCount":    float64(3),
		"courseCampus":          "ALL",
		"courseCampusList":      []any{"全部校区", "全部校区"},
	}
	if got := bykcCourseStatus(course, now); got != bykcStatusAvailable {
		t.Fatalf("status = %q", got)
	}
	dto := mapBykcCourse(course, bykcStatusAvailable)
	if dto.CourseName != "博雅课" {
		t.Fatalf("courseName = %q", dto.CourseName)
	}
	if len(dto.AudienceCampuses) != 1 || dto.AudienceCampuses[0] != "未指定校区" {
		t.Fatalf("audienceCampuses = %#v", dto.AudienceCampuses)
	}
	course["courseCurrentCount"] = float64(10)
	if got := bykcCourseStatus(course, now); got != bykcStatusFull {
		t.Fatalf("full status = %q", got)
	}
	course["selected"] = true
	if got := bykcCourseStatus(course, now); got != bykcStatusSelected {
		t.Fatalf("selected status = %q", got)
	}
}

func TestBykcSignConfigAndAvailability(t *testing.T) {
	config := parseBykcSignConfig(`{
		"signStartDate": "2026-05-24 08:00:00",
		"signEndDate": "2026-05-24 13:00:00",
		"signOutStartDate": "2026-05-24 11:00:00",
		"signOutEndDate": "2026-05-24 14:00:00",
		"signPointList": [{"lat": 39.9, "lng": 116.3, "radius": 50}]
	}`)
	if config == nil || len(config.SignPoints) != 1 {
		t.Fatalf("config = %#v", config)
	}
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.Local)
	checkin := 0
	canSign, canSignOut := bykcAttendanceAvailability(config, &checkin, nil, now)
	if !canSign || !canSignOut {
		t.Fatalf("availability sign=%v signOut=%v", canSign, canSignOut)
	}
	pass := 1
	canSign, canSignOut = bykcAttendanceAvailability(config, &checkin, &pass, now)
	if canSign || canSignOut {
		t.Fatalf("passed availability sign=%v signOut=%v", canSign, canSignOut)
	}
}

func TestBykcAESRoundTripAndResponseDecode(t *testing.T) {
	key := []byte("1234567890abcdef")
	encrypted, err := aesECBEncrypt([]byte(`{"status":"0"}`), key)
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := aesECBDecrypt(encrypted, key)
	if err != nil {
		t.Fatal(err)
	}
	if string(decrypted) != `{"status":"0"}` {
		t.Fatalf("decrypted = %q", decrypted)
	}
	encoded := base64.StdEncoding.EncodeToString(encrypted)
	quoted := `"` + encoded + `"`
	if got := decodeBykcResponse(quoted, key); got != `{"status":"0"}` {
		t.Fatalf("decoded = %q", got)
	}
}

func TestBykcStatisticsMapping(t *testing.T) {
	raw := map[string]any{
		"validCount": float64(2),
		"statistical": map[string]any{
			"1|博雅课程": map[string]any{
				"2|德育": map[string]any{
					"assessmentCount":         float64(2),
					"completeAssessmentCount": float64(1),
				},
			},
		},
	}
	statistical := bykcMap(raw["statistical"])
	categories := []string{}
	for catKey, subRaw := range statistical {
		for subKey := range bykcMap(subRaw) {
			categories = append(categories, catKey[strings.LastIndex(catKey, "|")+1:]+"-"+subKey[strings.LastIndex(subKey, "|")+1:])
		}
	}
	if len(categories) != 1 || categories[0] != "博雅课程-德育" {
		t.Fatalf("categories = %#v", categories)
	}
}
