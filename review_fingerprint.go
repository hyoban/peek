package main

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type reviewFile struct {
	path         string
	worktreeMode string
	submodule    bool
}

// Parse only porcelain v2's fixed fields; a path may contain spaces,
// newlines, or invalid UTF-8. Renames carry a second NUL-delimited path.
func reviewFiles(status string) ([]reviewFile, error) {
	var files []reviewFile
	records := strings.Split(status, "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			continue
		}
		if strings.HasPrefix(record, "? ") {
			files = append(files, reviewFile{path: record[2:]})
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
		files = append(files, reviewFile{
			path: fields[count-1], worktreeMode: fields[modeField],
			submodule: strings.HasPrefix(fields[2], "S"),
		})
	}
	return files, nil
}

func hashReviewFiles(repo, status string, fingerprint hash.Hash) error {
	files, err := reviewFiles(status)
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
				head, err := reviewGit(p, maxReviewMetadata, "rev-parse", "--verify", "HEAD")
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
			return errReviewChanged
		}
		fingerprint.Write(content.Sum(nil))
	}
	return nil
}
