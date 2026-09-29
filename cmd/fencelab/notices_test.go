package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDependencyNoticeInventory(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "licenses", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []struct {
		Component, Version, Source string
		Notices                    []struct{ Path, SHA256 string }
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	for _, entry := range entries {
		key := entry.Component + "@" + entry.Version
		if covered[key] || len(entry.Notices) == 0 || !strings.HasPrefix(entry.Source, "https://") {
			t.Fatal("invalid component inventory", key)
		}
		covered[key] = true
		for _, notice := range entry.Notices {
			if !fs.ValidPath(notice.Path) || !strings.HasPrefix(notice.Path, "licenses/") {
				t.Fatal("invalid notice path", notice.Path)
			}
			text, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(notice.Path)))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(text)
			if hex.EncodeToString(sum[:]) != notice.SHA256 {
				t.Fatal("upstream notice bytes changed", notice.Path)
			}
		}
	}
	output, err := exec.Command("go", "list", "-deps", "-f", "{{if .Module}}{{.Module.Path}}@{{.Module.Version}}{{end}}", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("enumerate dependencies: %v: %s", err, output)
	}
	for _, key := range strings.Fields(string(output)) {
		if key == "github.com/PoojaAgarwal2003/FenceLab@" {
			continue
		}
		if !covered[key] {
			t.Error("linked dependency lacks notices:", key)
		}
	}
}
