package app_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	signCall    = "hanko.Sign("
	signingFile = "services/fude/internal/app/approve.go"
)

// TestHankoIsStampedOnlyByApprove walks the repository and fails if any code
// other than approve.go can stamp a Hanko. scripts/check-hanko-sign.sh runs the
// same rule in CI; this keeps it in `go test` too.
func TestHankoIsStampedOnlyByApprove(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	var callers []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "hanko":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), signCall) {
			rel, _ := filepath.Rel(root, path)
			callers = append(callers, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(callers) != 1 || callers[0] != signingFile {
		t.Fatalf("%s must be called only from %s, found in %v", signCall, signingFile, callers)
	}
}
