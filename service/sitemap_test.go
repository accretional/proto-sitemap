package sitemap

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/prototext"

	"github.com/accretional/xmile/service"
)

const urlsetDoc = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://example.com/</loc>
    <lastmod>2026-07-03</lastmod>
    <changefreq>daily</changefreq>
    <priority>0.8</priority>
  </url>
  <url>
    <loc>https://example.com/about</loc>
    <priority>0.5</priority>
  </url>
</urlset>`

const indexDoc = `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap>
    <loc>https://example.com/sitemap1.xml.gz</loc>
    <lastmod>2026-07-03T12:00:00+00:00</lastmod>
  </sitemap>
  <sitemap>
    <loc>https://example.com/sitemap2.xml.gz</loc>
  </sitemap>
</sitemapindex>`

// A sitemap carrying Google's namespaced image extension: the core must project
// and the namespaced markup must pass through (the schema is open), not error.
const extensionDoc = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">
  <url>
    <loc>https://example.com/page</loc>
    <image:image>
      <image:loc>https://example.com/photo.jpg</image:loc>
    </image:image>
  </url>
</urlset>`

func parser(t *testing.T) *service.Parser {
	t.Helper()
	p, err := service.Default()
	if err != nil {
		t.Fatalf("Default parser: %v", err)
	}
	return p
}

func TestProcessURLSet(t *testing.T) {
	p := parser(t)
	msg, root, err := Process(p, urlsetDoc)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if root != "urlset" {
		t.Errorf("root = %q, want urlset", root)
	}
	got := prototext.Format(msg)
	for _, want := range []string{"https://example.com/", "https://example.com/about", "0.8", "daily", "2026-07-03"} {
		if !strings.Contains(got, want) {
			t.Errorf("typed projection missing %q; got:\n%s", want, got)
		}
	}
}

func TestProcessIndex(t *testing.T) {
	p := parser(t)
	msg, root, err := Process(p, indexDoc)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if root != "sitemapindex" {
		t.Errorf("root = %q, want sitemapindex", root)
	}
	got := prototext.Format(msg)
	for _, want := range []string{"sitemap1.xml.gz", "sitemap2.xml.gz"} {
		if !strings.Contains(got, want) {
			t.Errorf("typed projection missing %q; got:\n%s", want, got)
		}
	}
}

func TestProcessToleratesExtensions(t *testing.T) {
	p := parser(t)
	msg, root, err := Process(p, extensionDoc)
	if err != nil {
		t.Fatalf("Process should tolerate namespaced extensions: %v", err)
	}
	if root != "urlset" {
		t.Errorf("root = %q, want urlset", root)
	}
	// The core <loc> still projects even though the extension markup is dropped.
	if got := prototext.Format(msg); !strings.Contains(got, "https://example.com/page") {
		t.Errorf("core <loc> not projected; got:\n%s", got)
	}
}

func TestProcessRejectsNonSitemap(t *testing.T) {
	p := parser(t)
	if _, _, err := Process(p, `<rss version="2.0"><channel/></rss>`); err == nil {
		t.Fatal("Process accepted a non-sitemap root; want a ValidityError")
	}
}

// Round-trip: Parse(Generate(Parse(b))) == Parse(b) at the canonical infoset.
func TestRoundTrip(t *testing.T) {
	p := parser(t)
	for name, doc := range map[string]string{"urlset": urlsetDoc, "index": indexDoc, "extension": extensionDoc} {
		x1, err := Parse(p, doc)
		if err != nil {
			t.Fatalf("%s: Parse: %v", name, err)
		}
		ok, out, err := RoundTrip(p, x1)
		if err != nil {
			t.Fatalf("%s: RoundTrip: %v", name, err)
		}
		if !ok {
			t.Errorf("%s: round-trip changed the AST\n--- regenerated ---\n%s", name, out)
		}
	}
}

// A lowercase encoding declaration must still round-trip: Generate canonicalizes
// it to UTF-8, and the comparison is at the canonical infoset.
func TestRoundTripLowercaseEncoding(t *testing.T) {
	p := parser(t)
	const doc = `<?xml version="1.0" encoding="utf-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://example.com/</loc></url></urlset>`
	x, err := Parse(p, doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ok, out, err := RoundTrip(p, x)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if !ok {
		t.Errorf("lowercase encoding did not round-trip\n%s", out)
	}
}

func TestConformanceClean(t *testing.T) {
	p := parser(t)
	for name, doc := range map[string]string{"urlset": urlsetDoc, "index": indexDoc} {
		x, err := Parse(p, doc)
		if err != nil {
			t.Fatalf("%s: Parse: %v", name, err)
		}
		if w := Conformance(x); len(w) != 0 {
			t.Errorf("%s: expected no warnings, got %v", name, w)
		}
	}
}

func TestConformanceCatchesViolations(t *testing.T) {
	const bad = `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/</loc><changefreq>often</changefreq><priority>7</priority></url>
  <url><lastmod>not-a-date</lastmod></url>
</urlset>`
	p := parser(t)
	x, err := Parse(p, bad)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	w := strings.Join(Conformance(x), "\n")
	for _, want := range []string{"changefreq", "priority", "missing the required <loc>", "not a valid W3C Datetime"} {
		if !strings.Contains(w, want) {
			t.Errorf("expected a warning about %q; got:\n%s", want, w)
		}
	}
}

func TestLint(t *testing.T) {
	p := parser(t)
	warns, err := Lint(p, []byte(urlsetDoc))
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("clean urlset should lint with no warnings, got %v", warns)
	}
	if _, err := Lint(p, []byte(`<html><body>not xml-ish</body></html>`)); err == nil {
		t.Error("Lint accepted a non-sitemap; want an error")
	}
}
