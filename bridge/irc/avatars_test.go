package birc

import (
	"testing"
	"time"
)

func TestParseAvatarRefreshInterval(t *testing.T) {
	cases := []struct {
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{"", defaultAvatarRefreshInterval, false},
		{"30m", 30 * time.Minute, false},
		{"2h", 2 * time.Hour, false},
		{"0", 0, false},
		{"-5m", -5 * time.Minute, false},
		{"nonsense", 0, true},
	}
	for _, c := range cases {
		got, err := parseAvatarRefreshInterval(c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseAvatarRefreshInterval(%q): expected error", c.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseAvatarRefreshInterval(%q): unexpected error %s", c.raw, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseAvatarRefreshInterval(%q) = %s, want %s", c.raw, got, c.want)
		}
	}
}

func TestAvatarsToRefreshSkipsRehostedEntries(t *testing.T) {
	b := &Birc{avatarMap: make(map[string]string)}
	// Upstream URLs (to refresh) live under the hidden prefix.
	b.avatarMap[avatarOriginalPrefix+"alice"] = "https://src/a.png"
	b.avatarMap[avatarOriginalPrefix+"bob"] = "https://src/b.png"
	// Rehosted URLs live under the bare nick and must not be refreshed.
	b.avatarMap["alice"] = "https://media/a_123.png"
	b.avatarMap["bob"] = "https://media/b_456.png"

	got := b.avatarsToRefresh()
	want := map[string]string{
		"alice": "https://src/a.png",
		"bob":   "https://src/b.png",
	}
	if len(got) != len(want) {
		t.Fatalf("avatarsToRefresh returned %d entries, want %d: %v", len(got), len(want), got)
	}
	for nick, url := range want {
		if got[nick] != url {
			t.Errorf("avatarsToRefresh[%q] = %q, want %q", nick, got[nick], url)
		}
	}
}

func TestAvatarsToRefreshEmpty(t *testing.T) {
	b := &Birc{avatarMap: make(map[string]string)}
	if got := b.avatarsToRefresh(); len(got) != 0 {
		t.Fatalf("expected no avatars to refresh, got %v", got)
	}
}
