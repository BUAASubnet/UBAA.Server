package features

import (
	"encoding/json"
	"testing"
)

func TestLibBookEncryptReserveRequestMatchesKnownShape(t *testing.T) {
	encrypted := encryptLibBookJSON(`{"seat_id":"s1","segment":"am","day":"2026-05-24","start_time":"","end_time":""}`, "2026-05-24")
	if encrypted == "" {
		t.Fatal("empty encrypted payload")
	}
	if encrypted == `{"seat_id":"s1","segment":"am","day":"2026-05-24","start_time":"","end_time":""}` {
		t.Fatal("payload was not encrypted")
	}
	if got := string(libBookAESKey("2026-05-24")); got != "2026052442506202" {
		t.Fatalf("key = %q", got)
	}
}

func TestLibBookMappingHelpersMatchKtorBehavior(t *testing.T) {
	var areaInfo map[string]any
	raw := `{
		"data": {
			"area": {"id": "a1", "name": "三层东区"},
			"date": {"list": [
				{"day": "2026-05-24", "times": [
					{"id": "t1", "start": "08:00", "end": "12:00"}
				]},
				{"date": "2026-05-25", "times": []}
			]}
		}
	}`
	if err := json.Unmarshal([]byte(raw), &areaInfo); err != nil {
		t.Fatal(err)
	}
	detail := mapLibBookAreaDetail("fallback", areaInfo)
	if detail.ID != "a1" || detail.Name != "三层东区" {
		t.Fatalf("detail = %#v", detail)
	}
	if len(detail.AvailableDates) != 2 || detail.AvailableDates[1] != "2026-05-25" {
		t.Fatalf("availableDates = %#v", detail.AvailableDates)
	}
	if len(detail.TimeSlots) != 1 || detail.TimeSlots[0].Label != "08:00-12:00" {
		t.Fatalf("timeSlots = %#v", detail.TimeSlots)
	}

	booking := mapLibBookBooking(map[string]any{
		"id":         "b1",
		"name_merge": "沙河馆",
		"area_name":  "三层",
		"seat_no":    "A001",
		"date":       "2026-05-24",
		"begin_time": "08:00",
		"end_time":   "12:00",
	})
	if booking.NameMerge != "沙河馆" || booking.SeatNo != "A001" || booking.Day != "2026-05-24" {
		t.Fatalf("booking = %#v", booking)
	}
}

func TestLibBookMessageClassification(t *testing.T) {
	if !errorsIs(ErrLibBookSeatUnavailable, libBookMessageToError("座位已被预约")) {
		t.Fatal("seat unavailable classification failed")
	}
	if !errorsIs(ErrLibBookNotFound, libBookMessageToError("预约已取消")) {
		t.Fatal("not found classification failed")
	}
	if libBookBusinessSuccess(map[string]any{"code": float64(2), "message": "失败"}) {
		t.Fatal("business failure classified as success")
	}
}

func errorsIs(target, err error) bool {
	return target == err
}
