package crypt_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestImportBoundary(t *testing.T) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go executable is unavailable")
	}
	cmd := exec.Command(goPath, "list", "-deps", "-f", "{{.ImportPath}}", "github.com/borfast/gorfast/crypt/...")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, output)
	}
	for _, path := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.HasPrefix(path, "github.com/uptrace/") ||
			(strings.HasPrefix(path, "github.com/borfast/") && !strings.HasPrefix(path, "github.com/borfast/gorfast/crypt")) {
			t.Errorf("forbidden crypt dependency: %s", path)
		}
	}
}
