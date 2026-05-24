package features

import "testing"

func TestSpocMappingHelpersMatchKtorBehavior(t *testing.T) {
	if got := normalizeSpocDateTime("2026-03-11T15:59:00.000+00:00"); got != "2026-03-11 23:59:00" {
		t.Fatalf("date = %q", got)
	}
	if got := normalizeSpocScore("满分:100"); got != "100" {
		t.Fatalf("score = %q", got)
	}
	if got := spocSubmissionStatus("未做", true); got != "UNSUBMITTED" {
		t.Fatalf("status = %q", got)
	}
	if got := spocSubmissionStatusText("SUBMITTED", "1"); got != "已提交" {
		t.Fatalf("status text = %q", got)
	}
	plain := htmlPlainText("<p>请尽量给出自己的思考。</p>")
	if plain == nil || *plain != "请尽量给出自己的思考。" {
		t.Fatalf("plain text = %#v", plain)
	}
}

func TestSpocLoginTokenExtraction(t *testing.T) {
	raw := "https://spoc.buaa.edu.cn/spocnew/cas?token=abc&refreshToken=def"
	if got := extractSpocLoginToken(raw); got != "abc" {
		t.Fatalf("token = %q", got)
	}
}

func TestSpocParamEncryptionIsStableShape(t *testing.T) {
	encrypted := encryptSpocParam(`{"pageSize":15}`)
	if encrypted == "" {
		t.Fatal("empty encrypted param")
	}
	if encrypted == `{"pageSize":15}` {
		t.Fatal("param was not encrypted")
	}
}
