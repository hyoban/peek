package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peek/internal/testutil"

	"github.com/egoist/mygo/ui"
)

func TestSettingsFromTheChoicesOfBefore(t *testing.T) {
	dir := t.TempDir()
	if s := readSettings(dir); s != defaultSettings {
		t.Errorf("settings without a file %+v, want the defaults", s)
	}
	os.WriteFile(filepath.Join(dir, menuBarFile), nil, 0o644)
	if s := readSettings(dir); !s.MenuBar || s.HideInactive {
		t.Errorf("settings %+v, want the menu bar's choice of before", s)
	}
	os.WriteFile(filepath.Join(dir, settingsFile), []byte(`{"showInMenuBar": false, "wrapLines": false, "fetchEveryMinutes": 7}`), 0o644)
	s := readSettings(dir)
	if s.MenuBar || s.WrapLines || !s.FoldImports || s.FetchMinutes != defaultSettings.FetchMinutes {
		t.Errorf("settings %+v, want the file's, the defaults for the rest and an offered interval", s)
	}
}

func TestSettingsKept(t *testing.T) {
	dir := t.TempDir()
	p := &projects{apps: map[string]*app{}, inline: true, dir: dir}
	s := p.prefs()
	s.FetchMinutes, s.CommitModel = 15, "small"
	p.setPrefs(s)
	for i := 0; readSettings(dir) != s && i < 100; i++ {
		time.Sleep(10 * time.Millisecond) // written off the main thread
	}
	if got := readSettings(dir); got != s {
		t.Errorf("kept %+v, want %+v", got, s)
	}
	if p.fetchEvery() != 15*time.Minute {
		t.Errorf("fetched every %v, want 15 minutes", p.fetchEvery())
	}
}

func TestChangesUnfoldedAsChosen(t *testing.T) {
	repo := testutil.Repo(t)
	p := &projects{apps: map[string]*app{}, inline: true}
	s := defaultSettings
	s.FoldKinds, s.SideBySide = false, true
	p.setPrefs(s)
	p.open(repo)
	a := p.cur
	if !a.split {
		t.Error("the first project's diffs unified, want them side by side as chosen")
	}
	for _, d := range a.diffs {
		if d.read {
			t.Errorf("%s starts viewed, want every diff unfolded", d.change.Path)
		}
	}
}

func TestFilesUnnestedAsChosen(t *testing.T) {
	files := []string{"go.mod", "go.sum", "main.go"}
	if f := foldFiles(files, true); !f.nested["go.mod"] {
		t.Error("go.sum not nested under go.mod")
	}
	if f := foldFiles(files, false); len(f.nested) != 0 || len(f.dirs[""]) != 3 {
		t.Errorf("nested %v, roots %v, want all three files at the root", f.nested, f.dirs[""])
	}
}

func TestCodeAsChosen(t *testing.T) {
	s := defaultSettings
	s.WrapLines, s.FoldImports = false, false
	code := codeOf(codeBuffer("main.go", "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc main() { fmt.Println(os.Args) }\n"), s)
	if code.SoftWrap || len(code.Folded()) != 0 {
		t.Errorf("wrapped %v, folded %v, want neither", code.SoftWrap, code.Folded())
	}
}

// TestSettingsPanes builds each pane of the Settings window, light and
// dark, and draws them where PEEK_OUT tells:
//
//	PEEK_OUT=/tmp/settings go test ./internal/app -run SettingsPanes
func TestSettingsPanes(t *testing.T) {
	p := &projects{apps: map[string]*app{}, inline: true}
	p.settings = &settingsWindow{shown: p.prefs(), launch: launchChoices[0], layout: layoutChoices[0], fetch: minutesLabel(3), codex: "/usr/local/bin/codex"}
	tt := ui.NewTester(p.settingsView, settingsWidth, 420)
	tt.SetScale(2)
	tt.SetTitleBar(ui.TitleBar{Height: 28, Left: 68})
	out := os.Getenv("PEEK_OUT")
	for i, tab := range settingsTabs {
		p.settings.tab = i
		settle(tt)
		if p.settings.body == 0 {
			t.Errorf("%s laid out no pane", tab.name)
		}
		if out == "" {
			continue
		}
		for _, dark := range []bool{false, true} {
			tt.SetDark(dark)
			settle(tt)
			name := out + "-" + strings.ReplaceAll(strings.ToLower(tab.name), " ", "-")
			if dark {
				name += "-dark"
			}
			save(t, name+".png", tt.Image())
		}
		tt.SetDark(false)
	}
}

func TestCodeFontAndReviewsAsChosen(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, settingsFile), []byte(`{"codeFont": "Menlo", "codeFontSize": 15, "ignoreWhitespace": true, "reviewFailsAtPriority": 1}`), 0o644)
	s := readSettings(dir)
	if f := s.codeFont(); f.Family != "Menlo" || f.Size != 15 || !s.IgnoreWhitespace || s.ReviewBlocking != 1 {
		t.Errorf("settings %+v, font %+v, want Menlo at 15, whitespace ignored and reviews failing at P1", s, f)
	}
	os.WriteFile(filepath.Join(dir, settingsFile), []byte(`{"codeFontSize": 7, "reviewFailsAtPriority": 9}`), 0o644)
	s = readSettings(dir)
	if f := s.codeFont(); f.Family != monoFamily || f.Size != codeSize || s.ReviewBlocking != defaultSettings.ReviewBlocking {
		t.Errorf("font %+v, reviews at P%d, want the defaults for what is not offered", f, s.ReviewBlocking)
	}
}
