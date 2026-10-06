//go:build linux

// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestFrontendAssetsMatchSources(t *testing.T) {
	raw, err := assets.ReadFile("assets/generated/manifest.json")
	if err != nil {
		t.Fatal("frontend not built; run make ui-deps then make ui-build:", err)
	}
	var manifest struct {
		SourceHash string `json:"sourceHash"`
		React      string `json:"react"`
		Patternfly string `json:"patternfly"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..", "web")
	files := []string{"package.json", "package-lock.json", "build.mjs", ".npmrc"}
	for _, dir := range []string{"licenses", "src"} {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, entry := range entries {
			if !entry.IsDir() && !strings.Contains(entry.Name(), ".test.") {
				names = append(names, entry.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			files = append(files, dir+"/"+name)
		}
	}
	hash := sha256.New()
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		hash.Write([]byte(name))
		hash.Write(data)
	}
	if hex.EncodeToString(hash.Sum(nil)) != manifest.SourceHash {
		t.Fatal("stale embedded frontend; run make ui-build before Go compilation")
	}
	if manifest.React != "19.3.0" || manifest.Patternfly != "6.6.1" {
		t.Fatal("unexpected frontend versions")
	}
}

func TestEmbeddedStaticAssetsAreLocalAndConfined(t *testing.T) {
	handler := New(fakeCore{}).Handler()
	for _, test := range []struct {
		path    string
		status  int
		content string
	}{
		{"/assets/generated/app.js", 200, "javascript"}, {"/assets/generated/app.css", 200, "text/css"},
		{"/assets/generated/licenses.txt", 200, "text/plain"}, {"/assets/generated/missing.js", 404, ""},
		{"/assets/generated/", 404, ""}, {"/assets/index.html", 404, ""}, {"/etc/passwd", 404, ""},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9090"+test.path, nil)
		handler.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Errorf("%s status=%d", test.path, w.Code)
		}
		if test.content != "" && !strings.Contains(w.Header().Get("Content-Type"), test.content) {
			t.Errorf("wrong MIME for %s: %s", test.path, w.Header().Get("Content-Type"))
		}
		if strings.Contains(w.Header().Get("Content-Security-Policy"), "unsafe-") {
			t.Fatal("frontend must not weaken CSP")
		}
	}
}
