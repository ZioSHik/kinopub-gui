package gui

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.1.3", "v0.1.2", true},
		{"v0.2.0", "v0.1.9", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.1.2", "v0.1.2", false},
		{"v0.1.1", "v0.1.2", false},
		{"0.1.3", "0.1.2", true}, // missing v prefix
		{"v0.1.3", "dev", false}, // dev current never updates
		{"garbage", "v0.1.2", false},
		{"v0.1.3-rc1", "v0.1.2", true}, // pre-release suffix ignored
	}
	for _, c := range cases {
		if got := isNewer(c.latest, c.current); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	if _, _, _, ok := parseVersion("dev"); ok {
		t.Error("'dev' should not parse as a version")
	}
	if _, _, _, ok := parseVersion(""); ok {
		t.Error("empty should not parse")
	}
	maj, min, pat, ok := parseVersion("v2.3.4")
	if !ok || maj != 2 || min != 3 || pat != 4 {
		t.Errorf("parseVersion(v2.3.4) = %d.%d.%d ok=%v", maj, min, pat, ok)
	}
}

func TestAssetNameMatchesReleaseConvention(t *testing.T) {
	// The release workflow names GUI assets "kinopub-gui-<os>-<arch>[.exe]".
	got := assetName()
	if len(got) < len("kinopub-gui-") || got[:len("kinopub-gui-")] != "kinopub-gui-" {
		t.Errorf("assetName() = %q, want kinopub-gui-* prefix", got)
	}
}
