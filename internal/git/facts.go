package git

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// FileFacts are what reading a file of the work tree tells: the hash of
// its contents, which the review's snapshot fingerprints, and its lines,
// as git's numstat counts a new file's.
type FileFacts struct {
	Sum    [sha256.Size]byte
	Lines  int
	Binary bool // a NUL in the first 8000 bytes, as git tells binary files
}

// ErrFileChanged is what ReadFileFacts returns of a file that changed as
// it was read.
var ErrFileChanged = errors.New("the file changed as it was read")

// ReadFileFacts reads the regular file at p, which info is the Lstat of,
// once for all who ask: the line counts of the untracked files and the
// review's snapshot read the same files at once, thousands of them in a
// project with many it does not track, where opening them took most of
// the wait. A file as it was when read last keeps its facts, unless it
// changed within the second before, as git takes its index's entries.
func ReadFileFacts(p string, info os.FileInfo) (FileFacts, error) {
	facts.Lock()
	e := facts.files[p]
	if e != nil && (!unchangedSince(e.info, info) || e.done() && e.err != nil) {
		e = nil
	}
	if e == nil {
		e = &factsEntry{info: info, ready: make(chan struct{})}
		if len(facts.files) > 200_000 {
			clear(facts.files) // a bound, past what a project changes
		}
		facts.files[p] = e
		facts.Unlock()
		e.facts, e.err = readFacts(p, info)
		close(e.ready)
		if e.err != nil {
			facts.Lock()
			if facts.files[p] == e {
				delete(facts.files, p)
			}
			facts.Unlock()
		}
		return e.facts, e.err
	}
	facts.Unlock()
	<-e.ready
	return e.facts, e.err
}

var facts = struct {
	sync.Mutex
	files map[string]*factsEntry
}{files: map[string]*factsEntry{}}

type factsEntry struct {
	info  os.FileInfo
	ready chan struct{} // closed once read
	facts FileFacts
	err   error
}

func (e *factsEntry) done() bool {
	select {
	case <-e.ready:
		return true
	default:
		return false
	}
}

// opening bounds the files read at once: opened by many at once, each
// took several times as long, and all of them no less.
var opening = make(chan struct{}, 4)

func readFacts(p string, info os.FileInfo) (FileFacts, error) {
	opening <- struct{}{}
	defer func() { <-opening }()
	f, err := os.Open(p)
	if err != nil {
		return FileFacts{}, err
	}
	defer f.Close()
	h := sha256.New()
	var ff FileFacts
	buf := make([]byte, 32<<10)
	var read int
	var last byte
	for {
		n, err := f.Read(buf)
		chunk := buf[:n]
		if read < 8000 && bytes.IndexByte(chunk[:min(n, 8000-read)], 0) >= 0 {
			ff.Binary = true
		}
		h.Write(chunk)
		ff.Lines += bytes.Count(chunk, []byte{'\n'})
		if n > 0 {
			last = chunk[n-1]
		}
		read += n
		if err == io.EOF {
			break
		}
		if err != nil {
			return FileFacts{}, err
		}
	}
	if read > 0 && last != '\n' {
		ff.Lines++
	}
	after, err := os.Lstat(p)
	if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) || info.Mode() != after.Mode() {
		return FileFacts{}, ErrFileChanged
	}
	h.Sum(ff.Sum[:0])
	return ff, nil
}

// unchangedSince reports whether a file is still as it was: the same
// file, size, time and mode, that time not so recent that a write within
// it could leave them all as they were, as git takes its racy entries.
func unchangedSince(was, is os.FileInfo) bool {
	return was != nil && os.SameFile(was, is) && was.Size() == is.Size() && was.ModTime().Equal(is.ModTime()) &&
		was.Mode() == is.Mode() && time.Since(is.ModTime()) > time.Second
}
