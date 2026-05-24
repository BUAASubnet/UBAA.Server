package features

import (
	"encoding/json"
	"testing"
)

func TestCgyySignerMatchesKtorRules(t *testing.T) {
	timestamp := int64(1779620000000)
	sign := signCgyy("/api/orders/mine", map[string]any{
		"page":        0,
		"size":        20,
		"id":          99,
		"empty":       "",
		"list":        []any{"ignored"},
		"reservation": "A",
	}, timestamp)
	if sign == "" || len(sign) != 32 {
		t.Fatalf("sign = %q", sign)
	}
	if sign != signCgyy("api/orders/mine", map[string]any{"page": 0, "size": 20, "reservation": "A"}, timestamp) {
		t.Fatalf("sign cleanup/path normalization mismatch")
	}
	withNoCache := addCgyyNoCacheIfMissing(map[string]any{"page": 0}, timestamp)
	if withNoCache["nocache"] != timestamp {
		t.Fatalf("nocache = %#v", withNoCache)
	}
}

func TestCgyyDayInfoMappingMatchesKtorBehavior(t *testing.T) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(`{
		"spaceTimeInfo": [
			{"id": 1, "beginTime": "08:00", "endTime": "10:00"},
			{"id": 2, "beginTime": "10:00", "endTime": "12:00"}
		],
		"reservationDateSpaceInfo": {
			"2026-05-24": [
				{
					"id": 20,
					"spaceName": "B201",
					"venueSiteId": 7,
					"venueSpaceGroupId": 3,
					"1": {"reservationStatus": 1, "startDate": "2026-05-24 08:00"},
					"2": {"reservationStatus": 1, "tradeNo": "T"}
				}
			]
		},
		"ableReservationDateList": ["2026-05-24"],
		"token": "ctx-token",
		"reservationTotalNum": 4
	}`), &raw); err != nil {
		t.Fatal(err)
	}
	info := mapCgyyDayInfo(7, "2026-05-24", raw)
	if info.ReservationToken == nil || *info.ReservationToken != "ctx-token" {
		t.Fatalf("token = %#v", info.ReservationToken)
	}
	if len(info.TimeSlots) != 2 || info.TimeSlots[0].Label != "08:00-10:00" {
		t.Fatalf("time slots = %#v", info.TimeSlots)
	}
	if len(info.Spaces) != 1 || len(info.Spaces[0].Slots) != 2 {
		t.Fatalf("spaces = %#v", info.Spaces)
	}
	if !info.Spaces[0].Slots[0].IsReservable || info.Spaces[0].Slots[1].IsReservable {
		t.Fatalf("slots = %#v", info.Spaces[0].Slots)
	}
}

func TestCgyyPurposeTypesAndCaptchaEncryption(t *testing.T) {
	var raw any
	if err := json.Unmarshal([]byte(`{"content":[{"key":9,"name":"讲座、沙龙研讨类"},{"id":100,"name":"无关"}]}`), &raw); err != nil {
		t.Fatal(err)
	}
	types := parseCgyyPurposeTypes(raw)
	if len(types) != 1 || types[0].Key != 9 {
		t.Fatalf("types = %#v", types)
	}
	encrypted, err := encryptCgyyCaptcha(`{"x":10,"y":5}`, "1234567890abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "" || encrypted == `{"x":10,"y":5}` {
		t.Fatalf("encrypted = %q", encrypted)
	}
}
