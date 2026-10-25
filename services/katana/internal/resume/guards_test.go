package resume_test

import (
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/0xHoaxen/shogun/services/katana/internal/resume"
)

// A resume is read in memory and never stored (decision D4). This fails if the
// package starts using files, so adding that has to be a deliberate change here.
func TestThePackageNeverTouchesTheDisk(t *testing.T) {
	banned := map[string]bool{"os": true, "io/ioutil": true, "path/filepath": true, "os/exec": true, "net/http": true, "net": true}
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v (%d files)", err, len(files))
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range parsed.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if banned[path] {
				t.Errorf("%s imports %q: a resume must stay in memory", name, path)
			}
		}
	}
}

// seeds are one of each kind of file, good and damaged.
func seeds(t testing.TB) [][]byte {
	pdf := pdfOf([]string{textStream("Priya Raman", "Go")}, "")
	return [][]byte{
		[]byte("Priya Raman\nGo"),
		{},
		{0xFF, 0xFE, 'a', 0},
		docxOf(t, para("Priya Raman"), "head", "foot"),
		pdf, pdf[:len(pdf)/2], []byte("%PDF-1.4\n"), []byte("PK\x03\x04"),
		{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1},
	}
}

// FuzzExtract throws mutated files at Extract. Whatever it is given it must
// return a result or an *Error, never panic and never hang. `go test` runs the
// seeds; `go test -fuzz=FuzzExtract` mutates them.
func FuzzExtract(f *testing.F) {
	for _, seed := range seeds(f) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := resume.Extract(data)
		if err == nil && got.Text == "" {
			t.Fatal("no error and no text")
		}
		if err != nil {
			var e *resume.Error
			if !asResumeError(err, &e) {
				t.Fatalf("error %v (%T) is not a *resume.Error", err, err)
			}
		}
	})
}

func asResumeError(err error, target **resume.Error) bool { return errors.As(err, target) }

func TestEverySeedIsHandledWithoutAPanic(t *testing.T) {
	for i, seed := range seeds(t) {
		if _, err := resume.Extract(seed); err != nil {
			var e *resume.Error
			if !errors.As(err, &e) {
				t.Errorf("seed %d: %v is not a *resume.Error", i, err)
			}
		}
	}
}
