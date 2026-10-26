package git

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// catFile is a git cat-file --batch-command kept running, which reads the
// files of commits, as git show rev:path prints them, a request at a time:
// a git show took a process of its own for each side of each diff, and
// waiting for them most of the time a diff took to come.
type catFile struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
	// used counts the times it was put back, for an idle one to tell it
	// was not used since.
	used int
}

// catFiles are the processes not in use of each repository, a few at
// most, each closed once it has not been used for catFileIdle.
var catFiles = struct {
	sync.Mutex
	idle map[string][]*catFile
}{idle: map[string][]*catFile{}}

const (
	catFilesKept = 4
	catFileIdle  = 30 * time.Second
)

// errCatFile tells that a process could not answer, its request for git
// show to make instead.
var errCatFile = errors.New("git cat-file did not answer")

// showFile is git show rev:path's output, the file at the revision, or
// ErrTooLarge where it is longer than limit bytes, 0 for no limit, read
// through a process kept running, or by git show where one cannot.
func showFile(repo, object string, limit int) (string, error) {
	text, err := catFileText(repo, object, limit)
	if !errors.Is(err, errCatFile) {
		return text, err
	}
	if limit > 0 {
		return Bounded(repo, limit, "show", object)
	}
	return git(repo, "show", object)
}

func catFileText(repo, object string, limit int) (string, error) {
	// As many at once as other git commands may run, each taking a
	// process kept or starting one.
	gitSlots <- struct{}{}
	defer func() { <-gitSlots }()
	c, err := takeCatFile(repo)
	if err != nil {
		return "", errCatFile
	}
	text, err := c.read(object, limit)
	if errors.Is(err, errCatFile) {
		c.close() // its output out of step with its input
		return "", err
	}
	putCatFile(repo, c)
	return text, err
}

func takeCatFile(repo string) (*catFile, error) {
	catFiles.Lock()
	if idle := catFiles.idle[repo]; len(idle) > 0 {
		c := idle[len(idle)-1]
		catFiles.idle[repo] = idle[:len(idle)-1]
		catFiles.Unlock()
		return c, nil
	}
	catFiles.Unlock()
	cmd := exec.Command("git", "-C", repo, "cat-file", "--batch-command", "-z")
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &catFile{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 64<<10)}, nil
}

func putCatFile(repo string, c *catFile) {
	catFiles.Lock()
	defer catFiles.Unlock()
	if len(catFiles.idle[repo]) >= catFilesKept {
		go c.close()
		return
	}
	c.used++
	used := c.used
	catFiles.idle[repo] = append(catFiles.idle[repo], c)
	time.AfterFunc(catFileIdle, func() {
		catFiles.Lock()
		idle := catFiles.idle[repo]
		for i, o := range idle {
			if o == c && c.used == used {
				catFiles.idle[repo] = append(idle[:i:i], idle[i+1:]...)
				catFiles.Unlock()
				c.close()
				return
			}
		}
		catFiles.Unlock()
	})
}

func (c *catFile) close() {
	c.in.Close()
	c.cmd.Wait()
}

// read reads the object, a blob: an error as git show's where there is no
// such blob, ErrTooLarge past limit, and errCatFile where the process did
// not answer as it should.
func (c *catFile) read(object string, limit int) (string, error) {
	oid, size, err := c.ask("info", object)
	if err != nil {
		return "", err
	}
	if limit > 0 && size > limit {
		return "", ErrTooLarge
	}
	if _, size, err = c.ask("contents", oid); err != nil {
		return "", err
	}
	buf := make([]byte, size+1) // the contents, then a newline
	if _, err := io.ReadFull(c.out, buf); err != nil || buf[size] != '\n' {
		return "", errCatFile
	}
	return string(buf[:size]), nil
}

// ask sends a command of the object and reads the line that answers it:
// the object's name and size, where it is a blob.
func (c *catFile) ask(command, object string) (oid string, size int, err error) {
	if _, err := io.WriteString(c.in, command+" "+object+"\x00"); err != nil {
		return "", 0, errCatFile
	}
	line, err := c.out.ReadString('\n')
	if err != nil {
		return "", 0, errCatFile
	}
	line = strings.TrimSuffix(line, "\n")
	if strings.HasSuffix(line, " missing") || strings.HasSuffix(line, " ambiguous") {
		return "", 0, fmt.Errorf("git show %s: %s", object, line)
	}
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return "", 0, errCatFile
	}
	if size, err = strconv.Atoi(fields[2]); err != nil {
		return "", 0, errCatFile
	}
	if fields[1] != "blob" {
		if command == "contents" {
			c.out.Discard(size + 1) // what follows, to stay in step
		}
		return "", 0, fmt.Errorf("git show %s: a %s, not a file", object, fields[1])
	}
	return fields[0], size, nil
}
