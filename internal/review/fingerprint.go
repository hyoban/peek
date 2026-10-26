package review

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"peek/internal/git"
	"strings"
	"sync"
	"time"
)

type file struct {
	path         string
	worktreeMode string
	submodule    bool
}

// Parse only porcelain v2's fixed fields; a path may contain spaces,
// newlines, or invalid UTF-8. Renames carry a second NUL-delimited path.
func statusFiles(status string) ([]file, error) {
	var files []file
	records := strings.Split(status, "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			continue
		}
		if strings.HasPrefix(record, "? ") {
			files = append(files, file{path: record[2:]})
			continue
		}
		count, modeField := 0, 5
		switch record[0] {
		case '1':
			count = 9
		case '2':
			count = 10
			i++
		case 'u':
			count = 11
			modeField = 6
		default:
			return nil, fmt.Errorf("unsupported Git status record")
		}
		fields := strings.SplitN(record, " ", count)
		if len(fields) != count || fields[count-1] == "" {
			return nil, fmt.Errorf("invalid Git status record")
		}
		files = append(files, file{
			path: fields[count-1], worktreeMode: fields[modeField],
			submodule: strings.HasPrefix(fields[2], "S"),
		})
	}
	return files, nil
}

func hashFiles(repo, status string, fingerprint hash.Hash) error {
	files, err := statusFiles(status)
	if err != nil {
		return err
	}
	// The files hash at once, in the order they write to the fingerprint,
	// each one known since the last snapshot taken as it was then.
	sums.Lock()
	known := sums.repos[repo]
	sums.Unlock()
	parts := make([]filePart, len(files))
	next := make(chan int)
	var wg sync.WaitGroup
	for range min(8, len(files)) {
		wg.Go(func() {
			buffer := make([]byte, 32*1024)
			for i := range next {
				parts[i] = hashFile(repo, files[i], known[files[i].path], buffer)
			}
		})
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()
	kept := make(map[string]fileSum, len(files))
	for i, part := range parts {
		if part.err != nil {
			return part.err
		}
		fingerprint.Write(part.data)
		if part.sum.info != nil {
			kept[files[i].path] = part.sum
		}
	}
	sums.Lock()
	sums.repos[repo] = kept
	sums.Unlock()
	return nil
}

// sums are the content hashes of the files of each repository's last
// snapshot, which a file that has not changed since keeps: each refresh
// would else read every file changed again, thousands of them where a
// project has as many it does not track.
var sums = struct {
	sync.Mutex
	repos map[string]map[string]fileSum
}{repos: map[string]map[string]fileSum{}}

// fileSum is a file's content hash, as the file was when it was taken.
type fileSum struct {
	info os.FileInfo
	sum  []byte
}

// unchanged reports whether the file is still as it was when its hash was
// taken: the same file, size, time and mode, and that time not so recent
// that a write within it could have left them all as they were, as git
// takes its index's racy entries.
func (s fileSum) unchanged(info os.FileInfo) bool {
	return s.info != nil && os.SameFile(s.info, info) && s.info.Size() == info.Size() && s.info.ModTime().Equal(info.ModTime()) &&
		s.info.Mode() == info.Mode() && time.Since(info.ModTime()) > time.Second
}

// filePart is what a file writes to the fingerprint, and its hash to keep.
type filePart struct {
	data []byte
	sum  fileSum
	err  error
}

// hashFile is the part of the fingerprint of a file of the status.
func hashFile(repo string, entry file, known fileSum, buffer []byte) filePart {
	var part filePart
	write := func(format string, args ...any) { part.data = fmt.Appendf(part.data, format, args...) }
	write("%q\x00", entry.path)
	// A deleted tracked path can now name a directory, or have a parent
	// replaced by a file. Its replacement has separate status records.
	// Do not interpret that replacement as the deleted file's contents.
	if entry.worktreeMode == "000000" {
		write("deleted\x00")
		return part
	}
	p := filepath.Join(repo, entry.path)
	info, err := os.Lstat(p)
	if os.IsNotExist(err) {
		write("deleted\x00")
		return part
	}
	if err != nil {
		return filePart{err: err}
	}
	write("%s\x00", info.Mode())
	if info.Mode().IsRegular() && known.unchanged(info) {
		part.data, part.sum = append(part.data, known.sum...), known
		return part
	}
	content := sha256.New()
	switch {
	case entry.submodule && info.IsDir():
		if _, err := os.Lstat(filepath.Join(p, ".git")); os.IsNotExist(err) {
			content.Write([]byte("uninitialized"))
		} else if err != nil {
			return filePart{err: err}
		} else {
			head, err := git.Bounded(p, maxMetadata, "rev-parse", "--verify", "HEAD")
			if err != nil {
				return filePart{err: err}
			}
			content.Write([]byte(head))
		}
	case info.Mode()&os.ModeSymlink != 0:
		link, err := os.Readlink(p)
		if err != nil {
			return filePart{err: err}
		}
		content.Write([]byte(link))
	case info.Mode().IsRegular():
		file, err := os.Open(p)
		if err != nil {
			return filePart{err: err}
		}
		_, err = io.CopyBuffer(content, file, buffer)
		file.Close()
		if err != nil {
			return filePart{err: err}
		}
	default:
		return filePart{err: fmt.Errorf("cannot fingerprint %q", entry.path)}
	}
	after, err := os.Lstat(p)
	if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || info.ModTime() != after.ModTime() || info.Mode() != after.Mode() {
		return filePart{err: ErrChanged}
	}
	sum := content.Sum(nil)
	part.data = append(part.data, sum...)
	if info.Mode().IsRegular() {
		part.sum = fileSum{info: info, sum: sum}
	}
	return part
}

// Porcelain v2 exposes submodule work-tree and untracked dirt separately
// from a committed gitlink change. NUL records keep unusual paths intact.
func rejectDirtySubmodules(status string) error {
	records := strings.Split(status, "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if !strings.HasPrefix(record, "1 ") && !strings.HasPrefix(record, "2 ") && !strings.HasPrefix(record, "u ") {
			continue
		}
		fields := strings.SplitN(record, " ", 4)
		if len(fields) >= 3 {
			sub := fields[2]
			if len(sub) == 4 && sub[0] == 'S' && (sub[2] != '.' || sub[3] != '.') {
				return errors.New("cannot review uncommitted changes inside submodules; commit or discard those changes and review again")
			}
		}
		if record[0] == '2' {
			i++ // the original path of a rename is the next NUL record
		}
	}
	return nil
}
