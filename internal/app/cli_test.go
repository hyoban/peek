package app

import "testing"

func TestCLIName(t *testing.T) {
	for name, want := range map[string]string{"Peek": "peek", "Peek Dev": "peek-dev"} {
		if got := cliName(name); got != want {
			t.Errorf("cliName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestBundleOf(t *testing.T) {
	for exe, want := range map[string]string{
		"/Applications/Peek Dev.app/Contents/MacOS/Peek Dev": "/Applications/Peek Dev.app",
		"/tmp/go-build123/b001/exe/peek":                     "",
		"/opt/Contents/MacOS/peek":                           "",
	} {
		if got := bundleOf(exe); got != want {
			t.Errorf("bundleOf(%q) = %q, want %q", exe, got, want)
		}
	}
}

func TestViaLink(t *testing.T) {
	bundle := "/Applications/Peek.app"
	for arg0, want := range map[string]bool{
		"/Applications/Peek.app/Contents/MacOS/Peek": false, // LaunchServices
		"peek":                true, // found on PATH
		"/usr/local/bin/peek": true,
	} {
		if got := viaLink(arg0, bundle); got != want {
			t.Errorf("viaLink(%q) = %v, want %v", arg0, got, want)
		}
	}
	if viaLink("peek", "") {
		t.Error("go run's executable runs as the app")
	}
}
