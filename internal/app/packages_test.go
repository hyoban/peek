package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"peek/internal/definition"
	"peek/internal/git"
	"peek/internal/testutil"
)

type fakePackages struct {
	// installed finds a package from the offset of its name.
	installed func(offset int) []definition.Location
	links     []definition.Link
	found     []definition.Diagnostic
	asked     int
}

func (f *fakePackages) Installed(_ context.Context, _, _ string, offset int) ([]definition.Location, error) {
	if f.installed == nil {
		return nil, nil
	}
	return f.installed(offset), nil
}

func (f *fakePackages) Links(context.Context, string, string) ([]definition.Link, error) {
	return f.links, nil
}

func (f *fakePackages) Diagnostics(context.Context, string, string) ([]definition.Diagnostic, error) {
	f.asked++
	return f.found, nil
}

func (f *fakePackages) Close() {}

const (
	manifestBefore = "{\n  \"dependencies\": {\n    \"left\": \"1.0.0\"\n  }\n}\n"
	manifestAfter  = "{\n  \"dependencies\": {\n    \"right\": \"latest\",\n    \"left\": \"1.0.0\"\n  }\n}\n"
)

// manifestRepo has a package.json whose working tree adds a dependency,
// right, before left.
func manifestRepo(t *testing.T) string {
	repo := testutil.Repo(t)
	p := filepath.Join(repo, "package.json")
	os.WriteFile(p, []byte(manifestBefore), 0o644)
	testutil.Git(t, repo, "add", "package.json")
	testutil.Git(t, repo, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-m", "Manifest")
	os.WriteFile(p, []byte(manifestAfter), 0o644)
	return repo
}

// rangeOf is where s first is in the new manifest.
func rangeOf(s string) definition.Range {
	i := strings.Index(manifestAfter, s)
	return definition.Range{Start: definition.PositionAt(manifestAfter, i), End: definition.PositionAt(manifestAfter, i+len(s))}
}

func TestDependencyFindingsUnderAddedLines(t *testing.T) {
	fake := &fakePackages{found: []definition.Diagnostic{
		{Range: rangeOf(`"1.0.0"`), Severity: definition.SeverityHint, Message: "left can be upgraded"},
		{Range: rangeOf(`"latest"`), Severity: definition.SeverityWarning, Message: "right uses the latest tag"},
	}}
	a, tt := testApp(t, manifestRepo(t), func(a *app) { a.packages.client = fake })
	a.changeTree.show("package.json")
	settle(tt)
	tt.Frame()
	if !tt.HasText("right uses the latest tag") {
		t.Errorf("the added dependency's finding does not show, texts %q", tt.Texts())
	}
	if tt.HasText("left can be upgraded") {
		t.Error("a finding of a dependency the diff leaves shows")
	}
	asked := fake.asked
	tt.Frame()
	if fake.asked != asked {
		t.Error("npmx was asked again for the same text")
	}
}

func TestDependencyDiagnosticsInFile(t *testing.T) {
	fake := &fakePackages{found: []definition.Diagnostic{{Range: rangeOf(`"latest"`), Severity: definition.SeverityError, Message: "deprecated"}}}
	a, tt := testApp(t, manifestRepo(t), func(a *app) { a.packages.client = fake })
	a.setMode(modeFiles)
	tt.Frame()
	a.openInFiles(git.Worktree, "package.json")
	tt.Frame()
	tt.Frame()
	ds := a.code.Diagnostics()
	if len(ds) != 1 || ds[0].Message != "deprecated" || manifestAfter[ds[0].Start:ds[0].End] != "\"latest\"" {
		t.Fatalf("diagnostics %+v", ds)
	}
}

func TestGoToInstalledPackage(t *testing.T) {
	repo := manifestRepo(t)
	installed := filepath.Join(repo, "node_modules", "right", "package.json")
	os.MkdirAll(filepath.Dir(installed), 0o755)
	os.WriteFile(installed, []byte("{\"name\": \"right\"}\n"), 0o644)
	name := strings.Index(manifestAfter, "right")
	fake := &fakePackages{
		installed: func(offset int) []definition.Location {
			if offset < name || offset > name+len("right") {
				return nil
			}
			return []definition.Location{{Path: installed}}
		},
		links: []definition.Link{{Range: rangeOf(`"latest"`), Target: "https://npmx.dev/package/right/v/latest"}},
	}
	a, tt := testApp(t, repo, func(a *app) { a.packages.client = fake })
	t.Cleanup(a.close)
	var opened []string
	a.packages.open = func(url string) { opened = append(opened, url) }
	a.setMode(modeFiles)
	tt.Frame()
	a.openInFiles(git.Worktree, "package.json")
	tt.Frame()
	if a.code.OnDefinition == nil {
		t.Fatal("Command-click does not go to packages")
	}
	// Its version goes to it as its name does.
	a.code.OnDefinition(strings.Index(manifestAfter, "latest"))
	if a.openFile != "node_modules/right/package.json" && a.openFile != installed {
		t.Fatalf("opened %q, note %q", a.openFile, a.navigation.note)
	}
	if len(opened) != 0 {
		t.Fatalf("opened %q on the web", opened)
	}
	a.goBack(-1)
	if a.openFile != "package.json" {
		t.Fatalf("back to %q", a.openFile)
	}
	a.goPackage(name, true)
	if len(opened) != 1 || opened[0] != fake.links[0].Target {
		t.Fatalf("opened %q", opened)
	}
}

func TestOpenUninstalledPackage(t *testing.T) {
	fake := &fakePackages{links: []definition.Link{{Range: rangeOf(`"latest"`), Target: "https://npmx.dev/package/right/v/latest"}}}
	a, tt := testApp(t, manifestRepo(t), func(a *app) { a.packages.client = fake })
	t.Cleanup(a.close)
	var opened []string
	a.packages.open = func(url string) { opened = append(opened, url) }
	a.setMode(modeFiles)
	tt.Frame()
	a.openInFiles(git.Worktree, "package.json")
	tt.Frame()
	a.code.OnDefinition(strings.Index(manifestAfter, "right"))
	if len(opened) != 1 || opened[0] != fake.links[0].Target {
		t.Fatalf("opened %q", opened)
	}
	a.code.OnDefinition(strings.Index(manifestAfter, "left"))
	if len(opened) != 1 || a.navigation.note != "No package here" {
		t.Fatalf("opened %q, note %q", opened, a.navigation.note)
	}
}
