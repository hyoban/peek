package definition

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type messageSink struct{ bytes.Buffer }

func (*messageSink) Close() error { return nil }

func TestKotlinImportNotifications(t *testing.T) {
	for _, tc := range []struct{ state, want string }{
		{`{"phase":"FINISHED","folders":[{"status":"IMPORTED"}]}`, ""},
		{`{"phase":"FINISHED","folders":[{"status":"FAILED","message":"Maven unavailable"}]}`, "Maven unavailable"},
		{`{"phase":"CANCELLED"}`, "CANCELLED"},
	} {
		sink := &messageSink{}
		r := &rpc{in: sink, importDone: make(chan struct{}), done: make(chan struct{})}
		r.workspaceImported(json.RawMessage(`{"phase":"IN_PROGRESS"}`))
		select {
		case <-r.importDone:
			t.Fatal("did not wait for import")
		default:
		}
		// JetBrains emits explicit null IDs on notifications; never answer them.
		body := `{"id":null,"method":"intellij/workspaceImportState","params":` + tc.state + `}`
		wire := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
		if err := r.read(bufio.NewReader(strings.NewReader(wire))); !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		if sink.Len() != 0 {
			t.Fatalf("answered a notification: %s", sink.String())
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := r.waitImport(ctx)
		cancel()
		if (tc.want == "" && err != nil) || (tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want))) {
			t.Fatalf("import %s: %v", tc.state, err)
		}
	}
	r := &rpc{importDone: make(chan struct{}), done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.waitImport(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestKotlinServerCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("HOME", dir)
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	if _, _, err := serverCommand(dir, "kotlin"); err == nil || !strings.Contains(err.Error(), "brew install JetBrains/utils/kotlin-lsp") {
		t.Fatalf("missing server: %v", err)
	}
	binary := filepath.Join(dir, "kotlin-lsp")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Main.kt", "build.gradle.kts"} {
		language := Language(name)
		program, args, err := serverCommand(dir, language)
		if language != "kotlin" || err != nil || program != binary || len(args) != 1 || args[0] != "--stdio" {
			t.Fatalf("%s: %s %v %v", name, program, args, err)
		}
	}
}

func TestKotlinProjectRoot(t *testing.T) {
	for _, tc := range []struct {
		markers    []string
		file, root string
	}{
		{[]string{"apps/android/settings.gradle.kts", "apps/android/app/build.gradle.kts"}, "apps/android/app/src/Main.kt", "apps/android"},
		{[]string{"pom.xml", "module/pom.xml"}, "module/src/Main.kt", "."},
		{[]string{"service/build.gradle"}, "service/src/Main.kt", "service"},
		{nil, "src/Main.kt", "."},
		{[]string{"pom.xml"}, "../library/Main.kt", "."},
	} {
		repo := t.TempDir()
		for _, marker := range tc.markers {
			path := filepath.Join(repo, marker)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if got := kotlinRoot(repo, filepath.Join(repo, tc.file)); got != filepath.Join(repo, tc.root) {
			t.Fatalf("%s: root %s, want %s", tc.file, got, tc.root)
		}
	}
}

func TestInstalledKotlinServer(t *testing.T) {
	if os.Getenv("PEEK_KOTLIN_LSP_TEST") != "1" {
		t.Skip("set PEEK_KOTLIN_LSP_TEST=1 to test kotlin-lsp")
	}
	repo := t.TempDir()
	dir := filepath.Join(repo, "apps", "kotlin")
	sources := filepath.Join(dir, "src", "main", "kotlin")
	if err := os.MkdirAll(sources, 0700); err != nil {
		t.Fatal(err)
	}
	source := "package demo\nfun main() { val emoji = \"😀\"; val person = Person(); greet(person) }\n"
	target := "package demo\r\nclass Person\r\nfun greet(person: Person) = person\r\n"
	pom := `<project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion><groupId>demo</groupId><artifactId>navigation</artifactId><version>1.0</version><properties><kotlin.version>2.2.0</kotlin.version></properties><dependencies><dependency><groupId>org.jetbrains.kotlin</groupId><artifactId>kotlin-stdlib</artifactId><version>${kotlin.version}</version></dependency></dependencies><build><sourceDirectory>${project.basedir}/src/main/kotlin</sourceDirectory><plugins><plugin><groupId>org.jetbrains.kotlin</groupId><artifactId>kotlin-maven-plugin</artifactId><version>${kotlin.version}</version><executions><execution><id>compile</id><phase>compile</phase><goals><goal>compile</goal></goals></execution></executions></plugin></plugins></build></project>`
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte(pom), 0600); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"Main.kt": source, "Model.kt": target} {
		if err := os.WriteFile(filepath.Join(sources, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	client := New(repo)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, name := range []string{"greet", "Person", "greet"} {
		locs, err := client.Definition(ctx, filepath.Join(sources, "Main.kt"), source, strings.LastIndex(source, name))
		if err != nil || len(locs) == 0 {
			t.Fatalf("%s: %v %v", name, locs, err)
		}
		if filepath.Base(locs[0].Path) != "Model.kt" {
			t.Fatalf("%s: target %s", name, locs[0].Path)
		}
		at := OffsetAt(target, locs[0].Range.Start)
		if !strings.HasPrefix(target[at:], name) {
			t.Fatalf("%s: offset %d", name, at)
		}
	}
}
