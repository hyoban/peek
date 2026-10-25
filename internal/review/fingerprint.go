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
	buffer := make([]byte, 32*1024)
	for _, entry := range files {
		fmt.Fprintf(fingerprint, "%q\x00", entry.path)
		// A deleted tracked path can now name a directory, or have a parent
		// replaced by a file. Its replacement has separate status records.
		// Do not interpret that replacement as the deleted file's contents.
		if entry.worktreeMode == "000000" {
			fingerprint.Write([]byte("deleted\x00"))
			continue
		}
		p := filepath.Join(repo, entry.path)
		info, err := os.Lstat(p)
		if os.IsNotExist(err) {
			fingerprint.Write([]byte("deleted\x00"))
			continue
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(fingerprint, "%s\x00", info.Mode())
		content := sha256.New()
		switch {
		case entry.submodule && info.IsDir():
			if _, err := os.Lstat(filepath.Join(p, ".git")); os.IsNotExist(err) {
				content.Write([]byte("uninitialized"))
			} else if err != nil {
				return err
			} else {
				head, err := git.Bounded(p, maxMetadata, "rev-parse", "--verify", "HEAD")
				if err != nil {
					return err
				}
				content.Write([]byte(head))
			}
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			content.Write([]byte(link))
		case info.Mode().IsRegular():
			file, err := os.Open(p)
			if err != nil {
				return err
			}
			_, err = io.CopyBuffer(content, file, buffer)
			file.Close()
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("cannot fingerprint %q", entry.path)
		}
		after, err := os.Lstat(p)
		if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || info.ModTime() != after.ModTime() || info.Mode() != after.Mode() {
			return ErrChanged
		}
		fingerprint.Write(content.Sum(nil))
	}
	return nil
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
