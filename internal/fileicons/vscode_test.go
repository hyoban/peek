package fileicons

import (
	"os"
	"path/filepath"
	"testing"
)

const iconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path fill="#7dd3fc" d="M2 2h12v12H2z"/></svg>`

// fakeVSCode writes VS Code's settings, an icon theme's extension, the
// old version of it VS Code has not cleared yet, and a language VS Code
// comes with, returning what loadTheme reads.
func fakeVSCode(t *testing.T) (settings, extensions, builtin string) {
	dir := t.TempDir()
	write := func(name, data string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("settings.json", `{
	// JSON with comments, as VS Code writes it
	"workbench.iconTheme": "flow-dawn",
}`)
	write("extensions/extensions.json", `[{"identifier":{"id":"thang-nm.flow-icons"},"relativeLocation":"thang-nm.flow-icons-2.0.10"}]`)
	write("extensions/thang-nm.flow-icons-2.0.9/package.json", `{"contributes":{"iconThemes":[{"id":"flow-dawn","path":"./old.json"}]}}`)
	write("extensions/thang-nm.flow-icons-2.0.10/package.json", `{"contributes":{"iconThemes":[
		{"id":"flow-dim","path":"./dim.json"},{"id":"flow-dawn","path":"./dawn.json"}]}}`)
	write("extensions/thang-nm.flow-icons-2.0.10/dawn.json", `{
		"file": "file",
		"iconDefinitions": {
			"file": {"iconPath": "./dawn/file.svg"}, "__file": {"iconPath": "./dawn-light/file.svg"},
			"go": {"iconPath": "./dawn/go.svg"}, "go_mod": {"iconPath": "./dawn/go_mod.svg"},
			"test_ts": {"iconPath": "./dawn/test_ts.svg"}, "typescript": {"iconPath": "./dawn/typescript.svg"},
			"__typescript": {"iconPath": "./dawn-light/typescript.svg"}, "convex": {"iconPath": "./dawn/convex.svg"},
			"android": {"iconPath": "./dawn/android.svg"}, "shell": {"iconPath": "./dawn/shell.svg"},
			"broken": {"iconPath": "./dawn/broken.svg"},
			"folder": {"iconPath": "./dawn/folder.svg"}, "folder_open": {"iconPath": "./dawn/folder_open.svg"},
			"__folder": {"iconPath": "./dawn-light/folder.svg"}, "folder_src": {"iconPath": "./dawn/folder_src.svg"},
			"folder_src_open": {"iconPath": "./dawn/folder_src_open.svg"}, "folder_template": {"iconPath": "./dawn/folder_template.svg"}
		},
		"fileExtensions": {"go": "go", "ts": "typescript", "test.ts": "test_ts", "xyz": "broken"},
		"fileNames": {"go.mod": "go_mod", "convex/schema.ts": "convex", "AndroidManifest.xml": "android"},
		"languageIds": {"shellscript": "shell"},
		"folder": "folder", "folderExpanded": "folder_open",
		"folderNames": {"src": "folder_src", ".github/ISSUE_TEMPLATE": "folder_template"},
		"folderNamesExpanded": {"src": "folder_src_open"},
		"light": {"file": "__file", "fileExtensions": {"ts": "__typescript"}, "folder": "__folder"}
	}`)
	for _, name := range []string{"dawn/file", "dawn-light/file", "dawn/go", "dawn/go_mod", "dawn/test_ts", "dawn/typescript",
		"dawn-light/typescript", "dawn/convex", "dawn/android", "dawn/shell",
		"dawn/folder", "dawn/folder_open", "dawn-light/folder", "dawn/folder_src", "dawn/folder_src_open", "dawn/folder_template"} {
		write("extensions/thang-nm.flow-icons-2.0.10/"+name+".svg", iconSVG)
	}
	write("extensions/thang-nm.flow-icons-2.0.10/dawn/broken.svg", "not an svg")
	write("builtin/shellscript/package.json", `{"contributes":{"languages":[
		{"id":"shellscript","extensions":[".sh",".zsh"],"filenames":[".zshrc"]}]}}`)
	return filepath.Join(dir, "settings.json"), filepath.Join(dir, "extensions"), filepath.Join(dir, "builtin")
}

func TestVSCodeTheme(t *testing.T) {
	th := loadTheme(fakeVSCode(t))
	if th == nil {
		t.Fatal("no theme")
	}
	for _, c := range []struct {
		path string
		dark bool
		want string
	}{
		{"main.go", true, "go"},
		{"go.mod", true, "go_mod"},
		{"src/a.test.ts", true, "test_ts"},           // the longest extension first
		{"src/index.ts", true, "typescript"},         // dark
		{"src/index.ts", false, "__typescript"},      // and light
		{"main.go", false, "go"},                     // where the light theme says nothing
		{"convex/schema.ts", true, "convex"},         // by the folder it is in
		{"app/AndroidManifest.xml", true, "android"}, // whatever the case
		{"x.sh", true, "shell"},                      // by its language
		{".zshrc", true, "shell"},
		{"x.unknown", true, "file"},
		{"x.unknown", false, "__file"},
	} {
		if got := th.icon(c.path, c.dark); got != c.want {
			t.Errorf("icon(%q, %v) = %s, want %s", c.path, c.dark, got, c.want)
		}
	}
	for _, c := range []struct {
		path       string
		open, dark bool
		want       string
	}{
		{"src", false, true, "folder_src"},
		{"src", true, true, "folder_src_open"},
		{"app/src", false, true, "folder_src"},                     // by its name
		{"lib/main/src", false, true, "folder_src"},                // a compact folder by its last
		{".github/issue_template", false, true, "folder_template"}, // by the folder it is in
		{"lib", false, true, "folder"},
		{"lib", true, true, "folder_open"},
		{"lib", false, false, "__folder"},
		{"lib", true, false, "folder_open"},
	} {
		if got := th.folder(c.path, c.open, c.dark); got != c.want {
			t.Errorf("folder(%q, %v, %v) = %s, want %s", c.path, c.open, c.dark, got, c.want)
		}
	}
	if th.svg("go") == nil {
		t.Error("go's SVG did not load")
	}
	if th.svg("broken") != nil {
		t.Error("a broken SVG loaded")
	}
}

func TestVSCodeFor(t *testing.T) {
	vscode.Store(loadTheme(fakeVSCode(t)))
	defer vscode.Store(nil)
	if icon := For("main.go", true); icon.Name != "go" || icon.SVG == nil {
		t.Errorf("For(main.go) = %+v", icon)
	}
	if Folder("src", false, true) == nil {
		t.Error("no icon of src")
	}
	// Where the theme's SVG does not draw, file-icons' icon does.
	if icon := For("x.xyz", true); icon.SVG != nil || icon.Char == "" {
		t.Errorf("For(x.xyz) = %+v", icon)
	}
}

func TestVSCodeNoTheme(t *testing.T) {
	if Folder("src", false, true) != nil {
		t.Error("a folder's icon without a theme")
	}
	settings, extensions, builtin := fakeVSCode(t)
	os.WriteFile(settings, []byte(`{"workbench.iconTheme": "vs-seti"}`), 0o644)
	if loadTheme(settings, extensions, builtin) != nil {
		t.Error("a theme VS Code has no extension of loaded")
	}
	if loadTheme(filepath.Join(t.TempDir(), "settings.json"), extensions, builtin) != nil {
		t.Error("a theme loaded without VS Code's settings")
	}
}
