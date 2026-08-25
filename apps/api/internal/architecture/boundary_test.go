package architecture

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOrganizationDomainCannotImportPrivateEvent(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate architecture test")
	}
	internalRoot := filepath.Dir(filepath.Dir(currentFile))
	for _, packageName := range []string{"organization", "request", "httpapi"} {
		packagePath := filepath.Join(internalRoot, packageName)
		err := filepath.WalkDir(packagePath, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".go" {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(content), "internal/privateevent") {
				t.Errorf("forbidden privateevent dependency in %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", packageName, err)
		}
	}
}
