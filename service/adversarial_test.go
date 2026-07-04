package sitemap

// adversarial_test.go — hardening gates. These began as the adversarial review's
// opt-in demonstrations (ADR 0003); with the fixes in place they are permanent,
// GATING tests: the validation defects now warn, and large / deep / DOCTYPE-
// bearing payloads are refused. This is the large-payload coverage whose absence
// let the DoS through the first time.

import (
	"fmt"
	"strings"
	"testing"
)

func urlDoc(t *testing.T, inner string) string {
	t.Helper()
	return `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url>` + inner + `</url></urlset>`
}

// D1 fixed: <priority> must be a plain decimal in [0.0, 1.0]. NaN and non-decimal
// spellings now draw a warning; genuine values stay clean.
func TestPriority_RejectsInvalidNumbers(t *testing.T) {
	p := parser(t)
	for _, v := range []string{"NaN", "nan", "1e0", "1E-3", "0x1p-1", "+0.5", "1.5", "2"} {
		x, err := Parse(p, urlDoc(t, `<loc>https://e.com/</loc><priority>`+v+`</priority>`))
		if err != nil {
			t.Fatalf("priority=%q: Parse: %v", v, err)
		}
		if w := strings.Join(Conformance(x), "\n"); !strings.Contains(w, "priority") {
			t.Errorf("priority=%q should warn; got %q", v, w)
		}
	}
	for _, v := range []string{"0", "1", "0.0", "1.0", "0.5", "0.8"} {
		x, err := Parse(p, urlDoc(t, `<loc>https://e.com/</loc><priority>`+v+`</priority>`))
		if err != nil {
			t.Fatalf("priority=%q: Parse: %v", v, err)
		}
		if w := Conformance(x); len(w) != 0 {
			t.Errorf("valid priority=%q should be clean; got %v", v, w)
		}
	}
}

// D2 fixed: <loc> must be an http/https URL; other schemes now warn.
func TestLoc_RequiresHTTPScheme(t *testing.T) {
	p := parser(t)
	for _, v := range []string{"javascript:alert(1)", "data:text/html,x", "file:///etc/passwd", "mailto:a@b", "ftp://x/y"} {
		x, err := Parse(p, urlDoc(t, `<loc>`+v+`</loc>`))
		if err != nil {
			t.Fatalf("loc=%q: Parse: %v", v, err)
		}
		if w := strings.Join(Conformance(x), "\n"); !strings.Contains(w, "loc") {
			t.Errorf("loc=%q should warn; got %q", v, w)
		}
	}
	for _, v := range []string{"https://e.com/", "http://e.com/x?y=1"} {
		x, err := Parse(p, urlDoc(t, `<loc>`+v+`</loc>`))
		if err != nil {
			t.Fatalf("loc=%q: Parse: %v", v, err)
		}
		if w := Conformance(x); len(w) != 0 {
			t.Errorf("valid loc=%q should be clean; got %v", v, w)
		}
	}
}

// Boundary: a DOCTYPE is refused before parsing (entity-expansion defense), on
// every public entry point.
func TestBoundary_RejectsDoctype(t *testing.T) {
	p := parser(t)
	const doc = `<?xml version="1.0"?><!DOCTYPE urlset><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://e.com/</loc></url></urlset>`
	if _, err := Parse(p, doc); !isInputError(err) {
		t.Errorf("Parse should reject DOCTYPE with *InputError; got %v", err)
	}
	if _, _, err := Process(p, doc); !isInputError(err) {
		t.Errorf("Process should reject DOCTYPE with *InputError; got %v", err)
	}
	if _, err := Lint(p, []byte(doc)); !isInputError(err) {
		t.Errorf("Lint should reject DOCTYPE with *InputError; got %v", err)
	}
}

// Boundary: a billion-laughs bomb rides a DOCTYPE, so it is refused up front,
// fast — the vector that crashes/hangs an unguarded parser.
func TestBoundary_RejectsEntityBomb(t *testing.T) {
	p := parser(t)
	var d strings.Builder
	d.WriteString(`<!DOCTYPE urlset [` + "\n" + `<!ENTITY e0 "AAAAAAAA">` + "\n")
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&d, `<!ENTITY e%d "&e%d;&e%d;">`+"\n", i, i-1, i-1)
	}
	d.WriteString(`]>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>&e30;</loc></url></urlset>`)
	if _, err := Parse(p, d.String()); !isInputError(err) {
		t.Errorf("entity bomb should be refused as *InputError; got %v", err)
	}
}

// Boundary: source larger than the 50 MiB protocol maximum is refused.
func TestBoundary_RejectsOversized(t *testing.T) {
	if err := guardSource(strings.Repeat("x", MaxInputBytes+1)); !isInputError(err) {
		t.Errorf("oversized source should be *InputError; got %v", err)
	}
	// A DOCTYPE-free, in-bound document passes the guard.
	if err := guardSource(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"/>`); err != nil {
		t.Errorf("clean small source should pass the guard; got %v", err)
	}
}

// Boundary: deeply nested input is rejected (xmile's depth guard) rather than
// crashing the process.
func TestBoundary_RejectsDeepNesting(t *testing.T) {
	p := parser(t)
	depth := 20000
	src := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` +
		strings.Repeat("<url>", depth) + strings.Repeat("</url>", depth) + `</urlset>`
	if _, err := Parse(p, src); err == nil {
		t.Errorf("deep nesting should be rejected; got nil error")
	}
}

// A large but shallow, in-bound sitemap that merely exceeds the *soft* 50,000-
// entry limit still parses (the entry cap is conformance, not a resource bound);
// Conformance reports it. Confirms large legitimate payloads are not broken.
func TestLargeShallowSitemap_ParsesWithEntryWarning(t *testing.T) {
	p := parser(t)
	var b strings.Builder
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	for i := 0; i < MaxEntries+1000; i++ {
		fmt.Fprintf(&b, `<url><loc>https://e.com/%d</loc></url>`, i)
	}
	b.WriteString(`</urlset>`)
	_, root, err := Process(p, b.String())
	if err != nil {
		t.Fatalf("large shallow sitemap should parse; got %v", err)
	}
	if root != "urlset" {
		t.Fatalf("root = %q", root)
	}
	x, _ := Parse(p, b.String())
	if w := strings.Join(Conformance(x), "\n"); !strings.Contains(w, "exceeds the 50000 limit") {
		t.Errorf("expected an entry-count warning; got %q", w)
	}
}

// Pinned by-design behavior: a wrong-namespace <urlset> still projects (local-name
// match), with the namespace mismatch reported as a warning. ADR 0001.
func TestWrongNamespace_StillProjects(t *testing.T) {
	p := parser(t)
	doc := `<urlset xmlns="http://evil.example/not-a-sitemap"><url><loc>https://e.com/</loc></url></urlset>`
	_, root, err := Process(p, doc)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if root != "urlset" {
		t.Errorf("root = %q, want urlset", root)
	}
}

func isInputError(err error) bool {
	_, ok := err.(*InputError)
	return ok
}
