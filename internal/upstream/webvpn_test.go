package upstream

import (
	"strings"
	"testing"
)

func TestWebVPNURLRoundTrip(t *testing.T) {
	original := "https://byxt.buaa.edu.cn/jwapp/sys/homeapp/api/home/currentUser.do?x=1#top"
	encoded := ToWebVPNURL(original)
	if !strings.HasPrefix(encoded, "https://d.buaa.edu.cn/https/") {
		t.Fatalf("encoded URL = %s", encoded)
	}
	decoded := FromWebVPNURL(encoded)
	if decoded != original {
		t.Fatalf("decoded = %s, want %s", decoded, original)
	}
}

func TestIsSSOURLUnderstandsWebVPNURL(t *testing.T) {
	if !IsSSOURL(ToWebVPNURL("https://sso.buaa.edu.cn/login")) {
		t.Fatal("expected webvpn SSO URL to be detected")
	}
}

func TestWebVPNURLRoundTripRootPathWithPortQueryAndFragment(t *testing.T) {
	original := "https://iclass.buaa.edu.cn:8346/?loginName=abc%2Bdef%3D&type=jumpMyCenter#/MyCenter"
	encoded := ToWebVPNURL(original)
	decoded := FromWebVPNURL(encoded)
	if decoded != original {
		t.Fatalf("decoded = %s, want %s", decoded, original)
	}
}
