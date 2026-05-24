package version

import "testing"

func TestCompareVersionsMatchesKtorBehavior(t *testing.T) {
	cases := []struct {
		left  string
		right string
		want  int
	}{
		{"v1.5.0", "1.5.0", 0},
		{"1.4.9", "1.5.0", -1},
		{"1.6.0", "1.5.9", 1},
		{"1.5", "1.5.0", 0},
	}
	for _, tc := range cases {
		got := compareVersions(tc.left, tc.right)
		if tc.want == 0 && got != 0 {
			t.Fatalf("compareVersions(%q, %q) = %d", tc.left, tc.right, got)
		}
		if tc.want < 0 && got >= 0 {
			t.Fatalf("compareVersions(%q, %q) = %d", tc.left, tc.right, got)
		}
		if tc.want > 0 && got <= 0 {
			t.Fatalf("compareVersions(%q, %q) = %d", tc.left, tc.right, got)
		}
	}
}
