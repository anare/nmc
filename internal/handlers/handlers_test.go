package handlers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePrefersSpecificityThenUserHandlers(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bchandlers")
	config := `[archive broad]
match = *.gz
list = printf broad
extract = printf broad

[archive preferred]
match = *.zip
list = printf custom
extract = printf custom

[archive catchall]
match = *
list = printf catchall
extract = printf catchall
`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	set, problems := Load(path)
	if len(problems) != 0 {
		t.Fatalf("handler config problems = %v", problems)
	}
	handler, ok := set.Archive("bundle.tar.gz")
	if !ok || handler.Name != "tar" {
		t.Fatalf("broad custom handler beat specific shipped handler: %+v", handler)
	}
	handler, ok = set.Archive("bundle.zip")
	if !ok || handler.Name != "preferred" || handler.Source != "user" {
		t.Fatalf("same-specificity user handler did not win: %+v", handler)
	}
	openHandler, ok := set.NamedOpen("photo.png")
	if !ok || openHandler.Name != "images" {
		t.Fatalf("named open handler = %+v", openHandler)
	}
	if _, ok := set.NamedOpen("unknown.data"); ok {
		t.Fatal("catch-all handler was returned as a named handler")
	}
	if handler, ok := set.OpenWith("unknown.data"); !ok || handler.Name != "open-any" {
		t.Fatalf("open-with catch-all = %+v", handler)
	}
}

func TestEnsureConfigWritesOnlyWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", ".bchandlers")
	if err := EnsureConfig(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("handler config mode = %o", info.Mode().Perm())
	}
	if err := os.WriteFile(path, []byte("# user's config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureConfig(path); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "# user's config\n" {
		t.Fatalf("existing config was overwritten: %q err=%v", content, err)
	}
}

func TestHandlerPathsArePassedThroughEnvironment(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "should-not-exist")
	hostile := "path '$(touch " + marker + ")"
	output, err := Run(context.Background(), `printf '%s' "$FILE"`, map[string]string{"FILE": hostile})
	if err != nil || string(output) != hostile {
		t.Fatalf("handler output=%q err=%v", output, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("path was interpreted as shell text: %v", err)
	}
	if _, err := Run(context.Background(), "true", map[string]string{"UNSAFE": "value"}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported environment variable error = %v", err)
	}
}

func TestLoadReportsMalformedHandlerSectionsAndPatterns(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bchandlers")
	config := `[archive bad]
match = [
list = tar -tf "$ARCHIVE"
extract = tar -xf "$ARCHIVE"
`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	_, problems := Load(path)
	if len(problems) < 2 {
		t.Fatalf("malformed handler config problems = %v", problems)
	}
}
