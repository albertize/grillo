//go:build linux

// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
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
	files = append(files, "../internal/ui/assets/index.html")
	for _, name := range []string{"apple-touch-icon.png", "favicon.ico", "favicon.svg", "g-foglia-32.png", "g-foglia-64.png", "icon-192.png", "icon-512.png"} {
		files = append(files, "../media/g-icon/"+name)
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

func TestTerminalStyleNonceIsFreshAndStyleOnly(t *testing.T) {
	h := New(fakeCore{}).Handler()
	pattern := regexp.MustCompile(`name="terminal-style-nonce" content="([A-Za-z0-9_-]{43})"`)
	last := ""
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:9090/", nil))
		match := pattern.FindStringSubmatch(w.Body.String())
		if len(match) != 2 || match[1] == last {
			t.Fatal("missing or reused style nonce")
		}
		last = match[1]
		csp := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "style-src 'self' 'nonce-"+last+"'") || !strings.Contains(csp, "script-src 'self';") || strings.Contains(csp, "unsafe-") {
			t.Fatalf("unsafe nonce policy: %s", csp)
		}
	}
}

func TestBrandAssetsMatchSuppliedIcons(t *testing.T) {
	for _, name := range []string{"apple-touch-icon.png", "favicon.ico", "favicon.svg", "g-foglia-32.png", "g-foglia-64.png", "icon-192.png", "icon-512.png"} {
		original, err := os.ReadFile(filepath.Join("..", "..", "media", "g-icon", name))
		if err != nil {
			t.Fatal(err)
		}
		embedded, err := assets.ReadFile("assets/generated/icons/" + name)
		if err != nil || !bytes.Equal(embedded, original) {
			t.Fatalf("icon differs: %s (%v)", name, err)
		}
	}
	raw, err := assets.ReadFile("assets/generated/site.webmanifest")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		StartURL string `json:"start_url"`
		Icons    []struct {
			Src   string `json:"src"`
			Sizes string `json:"sizes"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.StartURL != "/" || len(manifest.Icons) != 2 || strings.Contains(string(raw), "token") {
		t.Fatal("invalid or credential-bearing webmanifest")
	}
	for _, icon := range manifest.Icons {
		if !strings.HasPrefix(icon.Src, "/assets/generated/icons/") {
			t.Fatal("external manifest icon")
		}
		if _, err := assets.ReadFile(strings.TrimPrefix(icon.Src, "/")); err != nil {
			t.Fatal(err)
		}
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
		{"/assets/generated/theme-init.js", 200, "javascript"}, {"/assets/generated/site.webmanifest", 200, "application/manifest+json"},
		{"/assets/generated/icons/favicon.svg", 200, "image/svg+xml"}, {"/assets/generated/icons/g-foglia-32.png", 200, "image/png"},
		{"/assets/generated/icons/apple-touch-icon.png", 200, "image/png"}, {"/favicon.ico", 200, "image/vnd.microsoft.icon"},
		{"/assets/generated/icons/g-foglia-2048.png", 404, ""}, {"/media/g-icon/favicon.svg", 404, ""},
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
