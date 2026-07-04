// Package sitemap parses Sitemaps-0.9 documents (a <urlset> or a
// <sitemapindex>) per https://www.sitemaps.org/protocol.html and as consumed by
// Google Search, riding xmile's XML engine and gluon's schema compiler.
//
// The design mirrors how xmile handles RSS 2.0: the sitemap *structure* is data
// (formats/sitemap.ebnf, an EBNF element vocabulary compiled on demand into a
// proto descriptor by xmile's service.CompileGrammar); the only Go a format
// needs is its irreducible, CFG-inexpressible semantics (validate.go).
//
// One XML parser (xmile's) parses bytes into the generic, lossless Tag tree;
// Process then projects that tree into the typed sitemap AST. Because the
// sitemap core vocabulary lives in the default namespace
// (xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"), the schema is compiled
// *open* rather than namespace-extensible: xmile's projector matches the core
// by local name (namespace-agnostic) and tolerates any unmodeled markup —
// Google's namespaced image:/video:/news:/xhtml: extensions pass through, and
// the full untyped tree is always available from a no-schema Parse. Generate is
// xmile's inverse of Parse, so a sitemap round-trips at the infoset level.
package sitemap

import (
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/accretional/proto-sitemap/formats"
	xmlpb "github.com/accretional/xmile/proto/pb/xml"
	"github.com/accretional/xmile/service"
)

// NamespaceURI is the Sitemaps 0.9 protocol namespace every conforming sitemap
// declares on its root element.
const NamespaceURI = "http://www.sitemaps.org/schemas/sitemap/0.9"

var (
	schemaOnce sync.Once
	schema     *service.Schema
	schemaErr  error
)

// Schema returns the compiled sitemap Schema — both the urlset and sitemapindex
// roots from formats/sitemap.ebnf — compiling it on first use and caching the
// result. It is compiled *open* (types the modeled core by local name, tolerates
// unmodeled/namespaced extension markup) and carries the structural pre-check
// that determines a document is a sitemap at all (validateSitemap).
func Schema() (*service.Schema, error) {
	schemaOnce.Do(func() {
		schema, schemaErr = service.CompileSchema(
			formats.Sitemap,
			xmlpb.SchemaLanguage_EBNF_VOCAB,
			service.SchemaOptions{Package: "sitemap"},
			false, // not nsExtensible: the core vocabulary is itself namespaced
		)
		if schemaErr == nil {
			schema.Open = true // type the core, pass extension markup through
			schema.PreValidate = validateSitemap
		}
	})
	return schema, schemaErr
}

// Parser returns a default xmile XML parser, ready for Parse/Process. It is a
// thin re-export of service.Default so callers need not import xmile directly.
func Parser() (*service.Parser, error) { return service.Default() }

// Parse parses sitemap source into the generic XML AST — the lossless,
// round-trippable Tag tree. It checks well-formedness only (the error is
// *service.WFError); it performs no sitemap-specific validation. Use Process to
// project into the typed AST, or Lint for the conformance rules.
func Parse(p *service.Parser, src string) (*xmlpb.Xml, error) {
	return p.Parse(src, false)
}

// Process parses sitemap source and projects it into the typed sitemap AST — a
// urlset or sitemapindex message, resolved from the root element. It enforces
// the structural pre-check (the root is <urlset> or <sitemapindex>); unmodeled
// markup is tolerated (the schema is open), so Google's namespaced extensions
// pass through. The result is a dynamic proto message (read it by reflection,
// e.g. URLCount / SitemapCount) and the root element's name. A document that is
// not a sitemap is a *service.ValidityError.
func Process(p *service.Parser, src string) (proto.Message, string, error) {
	sch, err := Schema()
	if err != nil {
		return nil, "", err
	}
	res, err := p.Process(src, sch, false)
	if err != nil {
		return nil, "", err
	}
	return res.Document, res.Root, nil
}

// Generate serializes a generic XML AST back to bytes — xmile's inverse of
// Parse. It round-trips at the infoset level: Parse(Generate(Parse(b))) equals
// Parse(b) (not byte-identical: entity spelling, quote style, and insignificant
// whitespace are not recorded). Only the generic AST round-trips; the typed
// projection is a read-only view.
func Generate(x *xmlpb.Xml) ([]byte, error) {
	return service.Generate(x)
}
