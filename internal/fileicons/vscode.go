package fileicons

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
)

// theme is a file icon theme of VS Code's whose icons are SVGs, as Flow
// Icons (https://github.com/thang-nm/Flow-Icons) is: Peek follows the one
// VS Code is set to, where it can, to draw what VS Code draws.
type theme struct {
	dir         string            // the theme file's, its icons' paths relative to it
	icons       map[string]string // an icon's id, its SVG's path
	dark, light associations
	// The languages of VS Code and its extensions, by a file's name and
	// by its extension, for icons by language.
	langNames, langExts map[string]string

	mu    sync.Mutex
	svgs  map[string]*ui.SVG // nil where one does not parse
	cache map[cacheKey]string
}

type cacheKey struct {
	path            string
	dark, dir, open bool
}

// associations are a theme's icons of files, by name, extension and
// language, and the icon of other files, and its icons of folders, closed
// and open, by name; names may hold the folders a file or a folder is in,
// as "convex/schema.ts".
type associations struct {
	File                string            `json:"file"`
	FileNames           map[string]string `json:"fileNames"`
	Extensions          map[string]string `json:"fileExtensions"`
	Languages           map[string]string `json:"languageIds"`
	Folder              string            `json:"folder"`
	FolderExpanded      string            `json:"folderExpanded"`
	FolderNames         map[string]string `json:"folderNames"`
	FolderNamesExpanded map[string]string `json:"folderNamesExpanded"`
}

var vscode atomic.Pointer[theme]

// FollowVSCode loads the file icon theme VS Code is set to, from the
// extension that has it, to draw files with in place of file-icons' icons.
// It keeps file-icons' where VS Code, or the theme, is not there, or its
// icons are not SVGs, as those of a font are not.
func FollowVSCode() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	t := loadTheme(
		filepath.Join(home, "Library/Application Support/Code/User/settings.json"),
		filepath.Join(home, ".vscode/extensions"),
		"/Applications/Visual Studio Code.app/Contents/Resources/app/extensions",
	)
	if t != nil {
		vscode.Store(t)
	}
}

var iconThemeSetting = regexp.MustCompile(`"workbench\.iconTheme"\s*:\s*"([^"]+)"`)

// manifest is what Peek reads of an extension's package.json.
type manifest struct {
	Contributes struct {
		IconThemes []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"iconThemes"`
		Languages []struct {
			ID         string   `json:"id"`
			Extensions []string `json:"extensions"`
			Filenames  []string `json:"filenames"`
		} `json:"languages"`
	} `json:"contributes"`
}

// loadTheme loads the icon theme the settings at settings name, from the
// extensions installed in extensions, or those VS Code comes with in
// builtin, which also say the languages of files.
func loadTheme(settings, extensions, builtin string) *theme {
	data, err := os.ReadFile(settings)
	if err != nil {
		return nil
	}
	m := iconThemeSetting.FindSubmatch(data)
	if m == nil {
		return nil
	}
	id := string(m[1])
	t := &theme{langNames: map[string]string{}, langExts: map[string]string{}, svgs: map[string]*ui.SVG{}}
	t.cache = map[cacheKey]string{}
	var themeFile string
	for _, dir := range append(extensionDirs(builtin), extensionDirs(extensions)...) {
		data, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err != nil {
			continue
		}
		var mf manifest
		if json.Unmarshal(data, &mf) != nil {
			continue
		}
		for _, it := range mf.Contributes.IconThemes {
			if it.ID == id {
				themeFile = filepath.Join(dir, it.Path)
			}
		}
		for _, l := range mf.Contributes.Languages {
			for _, e := range l.Extensions {
				e = strings.ToLower(strings.TrimPrefix(e, "."))
				if _, ok := t.langExts[e]; !ok {
					t.langExts[e] = l.ID
				}
			}
			for _, n := range l.Filenames {
				n = strings.ToLower(n)
				if _, ok := t.langNames[n]; !ok {
					t.langNames[n] = l.ID
				}
			}
		}
	}
	if themeFile == "" {
		return nil
	}
	data, err = os.ReadFile(themeFile)
	if err != nil {
		return nil
	}
	var doc struct {
		associations
		Definitions map[string]struct {
			IconPath string `json:"iconPath"`
		} `json:"iconDefinitions"`
		Light *associations `json:"light"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	t.dir = filepath.Dir(themeFile)
	t.icons = map[string]string{}
	for id, d := range doc.Definitions {
		if strings.HasSuffix(strings.ToLower(d.IconPath), ".svg") {
			t.icons[id] = d.IconPath
		}
	}
	if _, ok := t.icons[doc.File]; !ok {
		return nil // a theme of a font's glyphs, or of no icons
	}
	t.dark = lower(doc.associations)
	// The light theme's associations replace the dark one's they name.
	t.light = t.dark
	if l := doc.Light; l != nil {
		l := lower(*l)
		t.light = associations{
			File:                cmp(l.File, t.dark.File),
			FileNames:           over(t.dark.FileNames, l.FileNames),
			Extensions:          over(t.dark.Extensions, l.Extensions),
			Languages:           over(t.dark.Languages, l.Languages),
			Folder:              cmp(l.Folder, t.dark.Folder),
			FolderExpanded:      cmp(l.FolderExpanded, t.dark.FolderExpanded),
			FolderNames:         over(t.dark.FolderNames, l.FolderNames),
			FolderNamesExpanded: over(t.dark.FolderNamesExpanded, l.FolderNamesExpanded),
		}
	}
	return t
}

// extensionDirs returns the folders of the extensions in dir: those its
// extensions.json lists, as VS Code keeps the old versions of extensions
// it updates a while, or else every one.
func extensionDirs(dir string) []string {
	var dirs []string
	if data, err := os.ReadFile(filepath.Join(dir, "extensions.json")); err == nil {
		var list []struct {
			RelativeLocation string `json:"relativeLocation"`
		}
		if json.Unmarshal(data, &list) == nil {
			for _, e := range list {
				if e.RelativeLocation != "" {
					dirs = append(dirs, filepath.Join(dir, e.RelativeLocation))
				}
			}
			return dirs
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(dir, e.Name()))
		}
	}
	return dirs
}

// lower lowercases the names and extensions of a, as VS Code matches
// them whatever their case.
func lower(a associations) associations {
	low := func(m map[string]string) map[string]string {
		out := make(map[string]string, len(m))
		for k, v := range m {
			out[strings.ToLower(k)] = v
		}
		return out
	}
	a.FileNames, a.Extensions = low(a.FileNames), low(a.Extensions)
	a.FolderNames, a.FolderNamesExpanded = low(a.FolderNames), low(a.FolderNamesExpanded)
	return a
}

func over(base, top map[string]string) map[string]string {
	out := make(map[string]string, len(base))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range top {
		out[k] = v
	}
	return out
}

func cmp(s, or string) string {
	if s != "" {
		return s
	}
	return or
}

// icon returns the id of the icon of the file at p, a path within the
// repository, as VS Code picks it: by the name with the folders it is in,
// the nearest last, then by the name, then by its extensions, the longest
// first, then by its language, and else the icon of files.
func (t *theme) icon(p string, dark bool) string {
	return t.cached(cacheKey{path: p, dark: dark}, func(a associations, p string) string { return a.file(t, p) })
}

// folder returns the id of the icon of the folder at p, open or closed,
// as VS Code picks it: by its name with the folders it is in, then by its
// name, and else the icon of folders.
func (t *theme) folder(p string, open, dark bool) string {
	return t.cached(cacheKey{path: p, dark: dark, dir: true, open: open}, func(a associations, p string) string {
		names, id := a.FolderNames, a.Folder
		if open {
			names, id = a.FolderNamesExpanded, a.FolderExpanded
		}
		if found, ok := byName(names, p); ok {
			return found
		}
		return id
	})
}

func (t *theme) cached(k cacheKey, find func(a associations, p string) string) string {
	t.mu.Lock()
	id, ok := t.cache[k]
	t.mu.Unlock()
	if ok {
		return id
	}
	a := t.light
	if k.dark {
		a = t.dark
	}
	id = find(a, strings.ToLower(k.path))
	t.mu.Lock()
	t.cache[k] = id
	t.mu.Unlock()
	return id
}

// byName finds the icon of p in names: by the name with the folders it is
// in, the nearest last, then by the name.
func byName(names map[string]string, p string) (string, bool) {
	dirs := strings.Split(p, "/")
	for i := 0; i < len(dirs); i++ {
		if id, ok := names[strings.Join(dirs[i:], "/")]; ok {
			return id, true
		}
	}
	return "", false
}

func (a associations) file(t *theme, p string) string {
	name := path.Base(p)
	if id, ok := byName(a.FileNames, p); ok {
		return id
	}
	for i := 0; i < len(name); i++ {
		if name[i] != '.' {
			continue
		}
		if id, ok := a.Extensions[name[i+1:]]; ok {
			return id
		}
	}
	lang, ok := t.langNames[name]
	for i := 0; !ok && i < len(name); i++ {
		if name[i] == '.' {
			lang, ok = t.langExts[name[i+1:]]
		}
	}
	if id, found := a.Languages[lang]; ok && found {
		return id
	}
	return a.File
}

// svg returns the SVG of the icon of id, nil where it cannot be read.
func (t *theme) svg(id string) *ui.SVG {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.svgs[id]; ok {
		return s
	}
	var s *ui.SVG
	if rel, ok := t.icons[id]; ok {
		if data, err := os.ReadFile(filepath.Join(t.dir, rel)); err == nil {
			s, _ = ui.ParseSVG(data)
		}
	}
	t.svgs[id] = s
	return s
}
