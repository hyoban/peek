package app

import (
	"context"
	"strings"
	"time"

	"peek/internal/definition"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// npmx tells of the dependencies of package.json and the workspace files
// of pnpm and Yarn: Command-click goes to a package as installed, or opens
// it on npmx.dev where it is not, and what it finds of them, as a
// vulnerability or a deprecation, shows in the file and under the lines a
// diff adds.

type packageClient interface {
	Installed(context.Context, string, string, int) ([]definition.Location, error)
	Links(context.Context, string, string) ([]definition.Link, error)
	Diagnostics(context.Context, string, string) ([]definition.Diagnostic, error)
	Close()
}

// packagesState is npmx's for the window. Its server outlives refreshes:
// each query sends the text shown, and it keeps what the registry said.
type packagesState struct {
	client packageClient
	// open opens a package's page, as the window's services do.
	open func(url string)
	// code is the file whose diagnostics were asked for, at version.
	code    *editor.State
	version uint64
}

// dependencyNotes is what npmx found of a diff's new text, at version.
type dependencyNotes struct {
	of      *editor.Buffer
	version uint64
	found   []definition.Diagnostic
}

func (a *app) packageClient() packageClient {
	if a.packages.client == nil {
		a.packages.client = definition.New(a.repo)
	}
	return a.packages.client
}

func (a *app) closePackages() {
	if a.packages.client != nil {
		a.packages.client.Close()
		a.packages.client = nil
	}
	a.packages.code = nil
}

func (a *app) canOpenPackage() bool {
	return a.readableSource() && !a.previewing() && definition.Manifest(a.openFile) != ""
}

// goPackage goes to the installed package.json of the dependency at
// offset, or on its line, or opens the package on npmx.dev where it is not
// installed; web opens it there in any case.
func (a *app) goPackage(offset int, web bool) {
	if !a.canOpenPackage() || a.packages.open == nil {
		return
	}
	a.cancelNavigation()
	n := &a.navigation
	client, source, seq := a.packageClient(), a.navigationSource(), n.seq
	text, path := a.code.Buffer.String(), a.sourcePath(a.openFile)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	n.cancel, n.source, n.busy, n.note = cancel, &source, true, "Finding package…"
	a.background(func() func() {
		var installed []definition.Location
		var links []definition.Link
		var err error
		if !web {
			// npmx goes from a dependency's name; a click on its version
			// goes from the name the line starts with.
			installed, err = client.Installed(ctx, path, text, offset)
			if name := lineName(text, offset); err == nil && len(installed) == 0 && name != offset {
				installed, err = client.Installed(ctx, path, text, name)
			}
		}
		if err == nil && len(installed) == 0 {
			links, err = client.Links(ctx, path, text)
		}
		cancel()
		return func() {
			if seq != a.navigation.seq || !a.sourceIs(source) {
				return
			}
			n := &a.navigation
			n.busy, n.cancel = false, nil
			if err != nil {
				n.note = "Could not find package: " + err.Error()
				return
			}
			if len(installed) > 0 {
				a.openDefinition(installed[0])
				return
			}
			if link, ok := packageLink(text, links, offset); ok {
				n.note = ""
				a.packages.open(link.Target)
				return
			}
			n.note = "No package here"
		}
	})
}

// lineName is where the text of the line at offset starts, in its quotes:
// a dependency's name.
func lineName(text string, offset int) int {
	i := strings.LastIndexByte(text[:min(offset, len(text))], '\n') + 1
	for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
		i++
	}
	if i < len(text) && (text[i] == '"' || text[i] == '\'') {
		i++
	}
	return i
}

// packageLink is the link at offset, or else the one on its line: npmx
// links a dependency's version, and its name opens it too.
func packageLink(text string, links []definition.Link, offset int) (definition.Link, bool) {
	line := definition.PositionAt(text, offset).Line
	var near definition.Link
	found := false
	for _, l := range links {
		if definition.OffsetAt(text, l.Range.Start) <= offset && offset <= definition.OffsetAt(text, l.Range.End) {
			return l, true
		}
		if !found && l.Range.Start.Line == line {
			near, found = l, true
		}
	}
	return near, found
}

// checkFileDependencies asks npmx about the manifest the file pane
// shows, once for each text, and underlines what it finds.
func (a *app) checkFileDependencies() {
	code, p := a.code, a.codePath
	if code == nil || a.codeNote != "" || definition.Manifest(p) == "" {
		return
	}
	version := code.Buffer.Version()
	if a.packages.code == code && a.packages.version == version {
		return
	}
	a.packages.code, a.packages.version = code, version
	client, text, path := a.packageClient(), code.Buffer.String(), a.sourcePath(p)
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		found, err := client.Diagnostics(ctx, path, text)
		cancel()
		return func() {
			if err != nil || a.code != code || code.Buffer.Version() != version {
				return
			}
			ds := make([]editor.Diagnostic, 0, len(found))
			for _, d := range found {
				ds = append(ds, editor.Diagnostic{
					Start: definition.OffsetAt(text, d.Range.Start), End: definition.OffsetAt(text, d.Range.End),
					Severity: editorSeverity(d.Severity), Message: d.Message, Source: d.Source,
				})
			}
			code.SetDiagnostics(version, ds)
		}
	})
}

func editorSeverity(s definition.Severity) editor.Severity {
	switch s {
	case definition.SeverityWarning:
		return editor.SeverityWarning
	case definition.SeverityInformation:
		return editor.SeverityInfo
	case definition.SeverityHint:
		return editor.SeverityHint
	}
	return editor.SeverityError
}

// checkDiffDependencies asks npmx about the new text of a manifest's
// diff, once for each text.
func (a *app) checkDiffDependencies(d *fileDiff) {
	if d.state == nil || d.patch || definition.Manifest(d.change.Path) == "" {
		return
	}
	buf := d.state.New
	version := buf.Version()
	if d.deps.of == buf && d.deps.version == version {
		return
	}
	d.deps = dependencyNotes{of: buf, version: version}
	client, text, path := a.packageClient(), buf.String(), a.sourcePath(d.change.Path)
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		found, err := client.Diagnostics(ctx, path, text)
		cancel()
		return func() {
			if err == nil && d.deps.of == buf && d.deps.version == version && buf.Version() == version {
				d.deps.found = found
			}
		}
	})
}

// dependencyFindings are what npmx found on the lines the diff adds or
// changes: the dependencies it leaves as they were are not its doing.
func dependencyFindings(d *fileDiff) []definition.Diagnostic {
	if d.state == nil || d.deps.of != d.state.New || d.deps.version != d.state.New.Version() {
		return nil
	}
	var out []definition.Diagnostic
	hunks := d.state.Hunks()
	for _, f := range d.deps.found {
		for _, h := range hunks {
			if line := f.Range.Start.Line; h.NewStart <= line && line < h.NewEnd {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// dependencyCard is what npmx found under its line: red for an error, as
// a critical vulnerability or a deprecation, orange for a warning, the
// accent for the rest, as a newer version.
func dependencyCard(c *ui.Context, f definition.Diagnostic) {
	t := c.Theme()
	color := t.Accent
	switch f.Severity {
	case definition.SeverityError:
		color = system(c, systemRed)
	case definition.SeverityWarning:
		color = system(c, systemOrange)
	}
	annotationCard(c, color, func() {
		ui.Text(c, f.Message).FontSize(smallSize).TextColor(t.Text).Selectable().FillWidth()
	})
}
