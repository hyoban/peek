package app

import (
	"cmp"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"peek/internal/codex"
	"peek/internal/review"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// settings are the user's choices, which the Settings window shows and
// settings.json in the app's folder keeps.
type settings struct {
	// General.
	MenuBar      bool `json:"showInMenuBar"`
	HideInactive bool `json:"hideWhenInactive"`
	Reopen       bool `json:"reopenRepositories"` // at launch, else the welcome window
	Notify       bool `json:"notifications"`
	// Navigation.
	NestFiles bool `json:"nestRelatedFiles"`
	FoldKinds bool `json:"foldTestsDocsGenerated"` // tests, docs and generated files start viewed
	// Text Editing.
	CodeFont         string `json:"codeFont"`     // the family of code, "" for monoFamily
	CodeSize         int    `json:"codeFontSize"` // in points
	SideBySide       bool   `json:"sideBySide"`   // the diffs' layout, where no project shows to copy
	IgnoreWhitespace bool   `json:"ignoreWhitespace"`
	WrapLines        bool   `json:"wrapLines"` // in Files; diffs always wrap
	FoldImports      bool   `json:"foldImports"`
	// Source Control.
	AutoFetch    bool `json:"fetchAutomatically"`
	FetchMinutes int  `json:"fetchEveryMinutes"`
	FetchMetered bool `json:"fetchOnMeteredNetworks"`
	// Intelligence.
	CommitModel string `json:"commitMessageModel"` // "" for codex.CommitModel
	// ReviewBlocking is the lowest priority a finding fails a review at,
	// from 0 to 3.
	ReviewBlocking int `json:"reviewFailsAtPriority"`
}

// defaultSettings are Peek's choices before the user's.
var defaultSettings = settings{
	Reopen:         true,
	Notify:         true,
	NestFiles:      true,
	FoldKinds:      true,
	WrapLines:      true,
	FoldImports:    true,
	AutoFetch:      true,
	FetchMinutes:   3,
	CodeSize:       codeSize,
	ReviewBlocking: review.DefaultBlocking,
}

// fetchMinutes are the intervals the Settings window offers, and
// codeSizes the sizes of code.
var (
	fetchMinutes = []int{1, 3, 5, 15, 30}
	codeSizes    = []int{9, 10, 11, 12, 13, 14, 15, 16, 18, 20, 24}
)

// codeFont is the font of code as the user chose it.
func (s settings) codeFont() ui.Font {
	family := s.CodeFont
	if family == "" {
		family = monoFamily
	}
	return ui.Font{Family: family, Size: float32(s.CodeSize)}
}

const settingsFile = "settings.json"

// The files that told the two choices of the app menu before the
// Settings window, there while chosen.
const (
	menuBarFile      = "menu-bar"
	hideInactiveFile = "hide-when-inactive"
)

// readSettings reads the user's choices from the app's folder, the
// defaults for those it does not tell.
func readSettings(dir string) settings {
	s := defaultSettings
	data, err := os.ReadFile(filepath.Join(dir, settingsFile))
	if err != nil {
		s.MenuBar = exists(filepath.Join(dir, menuBarFile))
		s.HideInactive = exists(filepath.Join(dir, hideInactiveFile))
		return s
	}
	json.Unmarshal(data, &s)
	if !slices.Contains(fetchMinutes, s.FetchMinutes) {
		s.FetchMinutes = defaultSettings.FetchMinutes
	}
	if !slices.Contains(codeSizes, s.CodeSize) {
		s.CodeSize = defaultSettings.CodeSize
	}
	if s.ReviewBlocking < 0 || s.ReviewBlocking >= len(reviewChoices) {
		s.ReviewBlocking = defaultSettings.ReviewBlocking
	}
	return s
}

// settingsWrites keeps the writes of settings.json in order: only the
// latest is written.
var settingsWrites struct {
	sync.Mutex
	latest int
}

// writeSettings keeps the choices in the app's folder, off the main
// thread.
func (p *projects) writeSettings(s settings) {
	if p.dir == "" {
		return
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	dir := p.dir
	settingsWrites.Lock()
	settingsWrites.latest++
	n := settingsWrites.latest
	settingsWrites.Unlock()
	go func() {
		settingsWrites.Lock()
		defer settingsWrites.Unlock()
		if n != settingsWrites.latest {
			return
		}
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, settingsFile), append(data, '\n'), 0o644)
		// The files of before, settings.json telling the choices now.
		os.Remove(filepath.Join(dir, menuBarFile))
		os.Remove(filepath.Join(dir, hideInactiveFile))
	}()
}

// prefs are the user's choices, the defaults in tests.
func (p *projects) prefs() settings {
	if p == nil || p.chosen == nil {
		return defaultSettings
	}
	return *p.chosen
}

func (a *app) prefs() settings { return a.projects.prefs() }

// setPrefs takes the user's new choices, keeps them, and has the projects
// follow them.
func (p *projects) setPrefs(s settings) {
	old := p.prefs()
	if p.chosen == nil {
		p.chosen = new(settings)
	}
	*p.chosen = s
	if p.settings != nil {
		p.settings.shown = s
	}
	if s == old {
		return
	}
	p.writeSettings(s)
	if s.MenuBar != old.MenuBar {
		p.showMenuBar(s.MenuBar)
	}
	for _, a := range p.apps {
		if s.NestFiles != old.NestFiles {
			a.filesFor = -1 // listed again, as the files show
		}
		if s.WrapLines != old.WrapLines && a.code != nil {
			a.code.SoftWrap = s.WrapLines
		}
		if s.codeFont() != old.codeFont() {
			if a.code != nil {
				a.code.Font = s.codeFont()
			}
			for _, d := range a.diffs {
				if d.state != nil {
					d.state.Font = s.codeFont()
				}
			}
		}
		if s.IgnoreWhitespace != old.IgnoreWhitespace {
			for _, d := range a.diffs {
				if d.state != nil {
					d.state.IgnoreWhitespace = s.IgnoreWhitespace
				}
			}
		}
	}
	if s.AutoFetch && (!old.AutoFetch || s.FetchMinutes < old.FetchMinutes || s.FetchMetered && !old.FetchMetered) {
		for _, a := range p.apps {
			a.fetchIfDue()
		}
	}
	p.syncMenu()
	if p.win != nil {
		p.win.Update(func() {})
	}
}

// fetchAllowed reports whether remotes may be fetched in the background:
// as the user chose, and as the network allows (networkAllows), metered
// connections too where the user chose so.
func (p *projects) fetchAllowed() bool {
	s := p.prefs()
	if !s.AutoFetch {
		return false
	}
	n := mygo.Network.Status()
	return n.Online && (!n.Metered || s.FetchMetered)
}

// fetchEvery is how often the remote the branch tracks is fetched while
// its project shows, for the toolbar to tell of new commits there.
func (p *projects) fetchEvery() time.Duration {
	return time.Duration(p.prefs().FetchMinutes) * time.Minute
}

// settingsWindow is the Settings window, as Xcode's: a toolbar of panes,
// each a form of the user's choices, the window as tall as the pane.
type settingsWindow struct {
	win *mygo.Window
	tab int
	// shown are the choices as the form binds them, which each pass takes
	// (takeSettings), and launch, layout and fetch the pop-up buttons'.
	shown                 settings
	launch, layout, fetch string
	font, size, reviews   string
	// fonts are the monospaced families installed, read as the window
	// opens; login whether Peek opens at login, as the system has it.
	fonts           []string
	login, wasLogin bool
	codex           string  // where the Codex CLI is, "" for nowhere
	body            float32 // the pane's height, as laid out last
	height          int     // the content's, as the window was given it
}

// settingsTab is a pane, its SF Symbol in the toolbar over its name.
type settingsTab struct {
	name, symbol string
}

var settingsTabs = []settingsTab{
	{"General", "gearshape"},
	{"Navigation", "location"},
	{"Text Editing", "character.cursor.ibeam"},
	{"Source Control", "arrow.triangle.branch"},
	{"Intelligence", "sparkles"},
}

// The pop-up buttons' choices: reviewChoices by the lowest priority
// failing a review.
var (
	launchChoices = []string{"Reopen Repositories", "Show Welcome Window"}
	layoutChoices = []string{"Unified", "Side by Side"}
	reviewChoices = []string{"Fail on P0 Findings", "Fail on P0–P1 Findings", "Fail on P0–P2 Findings", "Fail on Any Finding"}
)

// defaultFont names the code's font where the user chose none.
const defaultFont = "Default"

func sizeLabel(size int) string { return strconv.Itoa(size) + " pt" }

// monospacedFamilies are the monospaced font families installed, which
// code can show in.
func monospacedFamilies() []string {
	var names []string
	for _, f := range ui.FontFamilies() {
		if f.Monospaced && !strings.HasPrefix(f.Name, ".") {
			names = append(names, f.Name)
		}
	}
	return names
}

func minutesLabel(m int) string {
	if m == 1 {
		return "Every Minute"
	}
	return "Every " + strconv.Itoa(m) + " Minutes"
}

const (
	settingsWidth = 640
	settingsInset = 72 // the room before the form's labels and after its controls
)

// showSettings opens the Settings window, or brings it to the front, as
// the app menu's Settings… (⌘,) does.
func (p *projects) showSettings() {
	if p.inline {
		return
	}
	if w := p.settings; w != nil {
		w.win.Focus()
		return
	}
	s := p.prefs()
	w := &settingsWindow{shown: s, launch: launchChoices[0], layout: layoutChoices[0], fetch: minutesLabel(s.FetchMinutes), height: 360}
	if !s.Reopen {
		w.launch = launchChoices[1]
	}
	if s.SideBySide {
		w.layout = layoutChoices[1]
	}
	w.font, w.size, w.reviews = cmp.Or(s.CodeFont, defaultFont), sizeLabel(s.CodeSize), reviewChoices[s.ReviewBlocking]
	w.login = mygo.App.OpenAtLogin()
	w.wasLogin = w.login
	w.codex, _ = codex.Executable()
	p.settings = w
	w.win = mygo.NewWindow(mygo.WindowOptions{
		Title:             settingsTabs[w.tab].name,
		Width:             settingsWidth,
		Height:            w.height,
		UseContentSize:    true,
		DisableResize:     true,
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		Content:           ui.View(p.settingsView),
	})
	win := w.win
	// The families, which take a while to list the first time.
	go func() {
		fonts := monospacedFamilies()
		win.Update(func() { w.fonts = fonts })
	}()
	win.OnClosed(func() {
		if p.settings != nil && p.settings.win == win {
			p.settings = nil
		}
	})
}

// setOpenAtLogin makes Peek open as the user logs in, or not, as a login
// item of the user's, which System Settings lists: the system keeps the
// choice, not settings.json.
func (p *projects) setOpenAtLogin(open bool) {
	w := p.settings
	if err := mygo.App.SetOpenAtLogin(open); err != nil {
		w.login = w.wasLogin
		go mygo.Dialog.Message(mygo.MessageOptions{Parent: w.win, Type: mygo.MessageWarning, Message: "Couldn’t Change Login Items", Detail: err.Error()})
		return
	}
	w.wasLogin = open
}

// takeSettings takes the choices the form changed, as the pass after the
// change starts.
func (p *projects) takeSettings() {
	w := p.settings
	s := w.shown
	s.Reopen = w.launch == launchChoices[0]
	s.SideBySide = w.layout == layoutChoices[1]
	for _, m := range fetchMinutes {
		if w.fetch == minutesLabel(m) {
			s.FetchMinutes = m
		}
	}
	s.CodeFont = w.font
	if s.CodeFont == defaultFont {
		s.CodeFont = ""
	}
	for _, size := range codeSizes {
		if w.size == sizeLabel(size) {
			s.CodeSize = size
		}
	}
	if i := slices.Index(reviewChoices, w.reviews); i >= 0 {
		s.ReviewBlocking = i
	}
	if w.login != w.wasLogin {
		p.setOpenAtLogin(w.login)
	}
	if s != p.prefs() {
		p.setPrefs(s)
	}
}

func (p *projects) settingsView(c *ui.Context) {
	w := p.settings
	if w == nil {
		return
	}
	p.takeSettings()
	t := c.Theme()
	// AppKit's settings window: the system's title bar and preference
	// toolbar, the panes its buttons, and a form of AppKit's controls.
	if w.win != nil {
		var items []mygo.ToolbarItem
		for i, tab := range settingsTabs {
			items = append(items, mygo.ToolbarItem{ID: tab.name, Symbol: tab.symbol, Label: tab.name, Click: func() {
				if w.tab != i {
					w.tab = i
					w.win.SetTitle(tab.name) // the Window menu’s name for it
				}
			}})
		}
		w.win.SetToolbar(&mygo.Toolbar{Items: items, Style: mygo.ToolbarPreference, Selected: settingsTabs[w.tab].name})
	}
	body := ui.Column(c).Key("pane:"+settingsTabs[w.tab].name).FillWidth().Padding(t.Space(5), 0, t.Space(5), 0)
	w.body = body.Bounds().H
	body.Children(func() {
		// One form, its labels aligned over the separators between its
		// sections.
		ui.Form(c, func() {
			switch w.tab {
			case 0:
				p.generalSettings(c)
			case 1:
				p.navigationSettings(c)
			case 2:
				p.textEditingSettings(c)
			case 3:
				p.sourceControlSettings(c)
			case 4:
				p.intelligenceSettings(c)
			}
		}).Gap(t.Space(4))
	})
	// The window as tall as the pane, its top where it is, as AppKit's
	// preference windows change panes.
	if w.body == 0 {
		// A pane just shown has no height before it is laid out: the
		// next pass reads it.
		c.Services().Invalidate()
	} else if h := int(math.Ceil(float64(w.body))); h != w.height && w.win != nil {
		// Asking the window its size here would wait on the thread this
		// builds on: the height it was last given is kept instead.
		w.height = h
		w.win.SetContentSize(settingsWidth, h)
	}
}

// settingsField is a field of a pane: its label, with a colon, ending a
// little before the window's middle, its control and its description
// after, as the forms of AppKit's preference windows.
func settingsField(c *ui.Context, label string, fn func()) ui.Element {
	return ui.Field(c, label+":", fn).Padding(0, settingsInset/2, 0, settingsInset)
}

// settingsSection separates a pane's sections, across the window.
func settingsSection(c *ui.Context) {
	ui.Divider(c)
}

func (p *projects) generalSettings(c *ui.Context) {
	w := p.settings
	s := &w.shown
	settingsField(c, "At Launch", func() {
		ui.NativePopUpButton(c.Key("launch"), &w.launch, launchChoices).Width(220)
	}).Description("A repository opened from Terminal or Finder opens either way.")
	settingsField(c, "Login", func() {
		ui.NativeCheckbox(c.Key("login"), &w.login, "Open Peek at login")
	}).Description("Shown in the menu bar, it opens there alone.")
	settingsSection(c)
	settingsField(c, "Menu Bar", func() {
		ui.NativeCheckbox(c.Key("menuBar"), &s.MenuBar, "Show Peek in the menu bar instead of the Dock")
	}).Description("Its menu lists recent repositories, their changes and commits to pull.")
	settingsField(c, "When Inactive", func() {
		ui.NativeCheckbox(c.Key("hideInactive"), &s.HideInactive, "Hide Peek as another app comes to the front")
	})
	settingsSection(c)
	settingsField(c, "Notifications", func() {
		ui.NativeCheckbox(c.Key("notify"), &s.Notify, "Notify when Ship, a review or an answer finishes")
	}).Description("Only while you are not looking at the repository.")
}

func (p *projects) navigationSettings(c *ui.Context) {
	s := &p.settings.shown
	settingsField(c, "Files", func() {
		ui.NativeCheckbox(c.Key("nest"), &s.NestFiles, "Nest related files")
	}).Description("Lists companions, such as go.sum, under their file, as VS Code’s file nesting does.")
	settingsSection(c)
	settingsField(c, "Changes", func() {
		ui.NativeCheckbox(c.Key("foldKinds"), &s.FoldKinds, "Fold tests, docs and generated files as viewed")
	}).Description("Their groups start closed; selecting a file still opens its diff.")
}

func (p *projects) textEditingSettings(c *ui.Context) {
	w := p.settings
	s := &w.shown
	settingsField(c, "Font", func() {
		ui.Row(c).Gap(c.Theme().Space(2)).AlignItems(ui.Center).Children(func() {
			fonts := append([]string{defaultFont}, w.fonts...)
			if !slices.Contains(fonts, w.font) {
				fonts = append(fonts, w.font) // until the families are read
			}
			ui.NativePopUpButton(c.Key("font"), &w.font, fonts).Width(220)
			var sizes []string
			for _, size := range codeSizes {
				sizes = append(sizes, sizeLabel(size))
			}
			ui.NativePopUpButton(c.Key("size"), &w.size, sizes)
		})
	}).Description("Of diffs and files. Default is MonoLisa where it is installed, else the system’s monospaced font.")
	settingsSection(c)
	settingsField(c, "Diff Layout", func() {
		ui.NativePopUpButton(c.Key("layout"), &w.layout, layoutChoices).Width(220)
	}).Description("For the first repository opened; View ▸ Show Side by Side (⌃⌘D) switches it.")
	settingsField(c, "Whitespace", func() {
		ui.NativeCheckbox(c.Key("ignoreWhitespace"), &s.IgnoreWhitespace, "Ignore whitespace in diffs")
	}).Description("Changes of indentation or spacing alone show no difference; the files’ counts still include them.")
	settingsSection(c)
	settingsField(c, "Files", func() {
		ui.Column(c).Gap(c.Theme().Space(1.5)).Children(func() {
			ui.NativeCheckbox(c.Key("wrap"), &s.WrapLines, "Wrap lines")
			ui.NativeCheckbox(c.Key("foldImports"), &s.FoldImports, "Fold imports")
		})
	}).Description("Diffs always wrap, so that they never scroll sideways.")
}

func (p *projects) sourceControlSettings(c *ui.Context) {
	w := p.settings
	s := &w.shown
	settingsField(c, "Remotes", func() {
		ui.NativeCheckbox(c.Key("fetch"), &s.AutoFetch, "Fetch and count incoming commits automatically")
	}).Description("On the toolbar, the Dock icon and the menu bar. Peek never merges by itself.")
	settingsField(c, "Fetch", func() {
		ui.NativePopUpButton(c.Key("fetchEvery"), &w.fetch, fetchLabels()).Width(220).Disabled(!s.AutoFetch)
	}).Description("Repositories without a window are fetched every 15 minutes.")
	settingsField(c, "Networks", func() {
		ui.NativeCheckbox(c.Key("metered"), &s.FetchMetered, "Fetch on metered connections").Disabled(!s.AutoFetch)
	}).Description("Such as a phone’s hotspot, or a network in Low Data Mode.")
}

func fetchLabels() []string {
	var labels []string
	for _, m := range fetchMinutes {
		labels = append(labels, minutesLabel(m))
	}
	return labels
}

func (p *projects) intelligenceSettings(c *ui.Context) {
	w := p.settings
	s := &w.shown
	t := c.Theme()
	settingsField(c, "Codex CLI", func() {
		if w.codex == "" {
			ui.Text(c, "Not found").TextColor(t.TextMuted)
			return
		}
		ui.Text(c, w.codex).Selectable()
	}).Description("Install Codex and sign in with codex login to ask, review and ship.")
	settingsSection(c)
	settingsField(c, "Commit Messages", func() {
		ui.NativeTextField(c.Key("commitModel"), &s.CommitModel, codex.CommitModel).Width(220)
	}).Description("The model Ship writes commit messages with. Reviews and conversations use your Codex settings.")
	settingsSection(c)
	settingsField(c, "Reviews", func() {
		ui.NativePopUpButton(c.Key("reviews"), &w.reviews, reviewChoices).Width(220)
	}).Description("P0 is data loss or a security hole, P1 a core feature broken, P2 a less common failure, P3 rare or cosmetic.")
}
