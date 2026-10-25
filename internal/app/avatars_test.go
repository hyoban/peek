package app

import "testing"

func TestAvatarURL(t *testing.T) {
	for email, want := range map[string]string{
		"41898282+github-actions[bot]@users.noreply.github.com": "https://avatars.githubusercontent.com/u/41898282?s=64",
		"hyoban@users.noreply.github.com":                       "https://github.com/hyoban.png?size=64",
		"ada@example.com":                                       "https://www.gravatar.com/avatar/HASH?s=64&d=404",
	} {
		if got := avatarURL(email, "HASH"); got != want {
			t.Errorf("avatarURL(%q) = %q, want %q", email, got, want)
		}
	}
}
