package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"peek/internal/git"

	"github.com/egoist/mygo"
)

// The command line tool is a link to the app's executable, in cliDir,
// named for the app as cliName names it: run as the link, the executable
// opens the repository in the app, a second copy of it never starting.
const cliDir = "/usr/local/bin"

// cliName is the command of the app named name: peek for Peek, and
// peek-dev for Peek Dev, which installs beside it.
func cliName(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", "-"))
}

// bundleOf returns the app the executable is the binary of, "" where it
// is not one's, as go run's.
func bundleOf(exe string) string {
	dir := filepath.Dir(exe)
	if filepath.Base(dir) != "MacOS" || filepath.Base(filepath.Dir(dir)) != "Contents" {
		return ""
	}
	bundle := filepath.Dir(filepath.Dir(dir))
	if filepath.Ext(bundle) != ".app" {
		return ""
	}
	return bundle
}

// viaLink tells that the executable runs as another path than its own,
// the command line tool's link: LaunchServices starts it by its path in
// the app.
func viaLink(arg0, bundle string) bool {
	return bundle != "" && !strings.HasPrefix(arg0, bundle+"/")
}

// forward opens the repository the folder in args holds, or the one the
// working directory is in, in the app, where the executable runs as the
// command line tool, and tells whether it did.
func forward(args []string) bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	bundle := bundleOf(exe)
	if !viaLink(os.Args[0], bundle) {
		return false
	}
	app := strings.TrimSuffix(filepath.Base(bundle), ".app")
	name := cliName(app)
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Printf("usage: %s [path]\n\nOpens the repository at path, or the one the working directory is in, in %s.\n", name, app)
		os.Exit(0)
	}
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir) // a file's repository
	}
	repo, err := git.RepoRoot(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s is not in a Git repository\n", name, dir)
		os.Exit(1)
	}
	// As Finder opens the folder with the app: the app running takes it,
	// else starts with it.
	if out, err := exec.Command("open", "-a", bundle, repo).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", name, strings.TrimSpace(string(out)))
		os.Exit(1)
	}
	os.Exit(0)
	return true
}

// installCLI links the command line tool to the app's executable, asking
// for an administrator's password where the user cannot write cliDir, and
// tells how it went.
func installCLI(parent *mygo.Window) {
	name := cliName(mygo.App.Name())
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err == nil && bundleOf(exe) == "" {
		err = errors.New("Run " + mygo.App.Name() + " from its app to install the command.")
	}
	link := filepath.Join(cliDir, name)
	if err == nil {
		err = linkCLI(exe, link)
		if errors.Is(err, fs.ErrPermission) {
			err = linkCLIAsAdmin(exe, link)
		}
	}
	if err != nil {
		if strings.Contains(err.Error(), "User canceled") {
			return
		}
		mygo.Dialog.Message(mygo.MessageOptions{
			Parent:  parent,
			Type:    mygo.MessageWarning,
			Message: "The " + name + " command could not be installed",
			Detail:  err.Error(),
		})
		return
	}
	mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  parent,
		Message: "The " + name + " command is installed",
		Detail:  "Run " + name + " in a repository, or " + name + " with a folder, to open it in " + mygo.App.Name() + ".",
	})
}

// linkCLI links link to exe, over what is there.
func linkCLI(exe, link string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	if err := os.Remove(link); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Symlink(exe, link)
}

// linkCLIAsAdmin links link to exe as an administrator, as the user
// authorizes in the system's dialog.
func linkCLIAsAdmin(exe, link string) error {
	sh := fmt.Sprintf("mkdir -p %s && ln -sf %s %s", shQuote(filepath.Dir(link)), shQuote(exe), shQuote(link))
	script := fmt.Sprintf("do shell script %q with administrator privileges", sh)
	if out, err := exec.Command("osascript", "-e", script).CombinedOutput(); err != nil {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
