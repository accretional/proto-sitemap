package sitemap

// guard.go — the input boundary. proto-sitemap parses sitemaps fetched from
// arbitrary servers, so it refuses two classes of input before handing bytes to
// xmile, independent of (and in depth over) xmile's own parser guards:
//
//   - Oversized source. The protocol caps a file at 50 MiB uncompressed; nothing
//     conformant is larger, and refusing larger input up front bounds the memory
//     an untrusted document can force the parser to allocate.
//   - Any DOCTYPE. Sitemaps have no legitimate DOCTYPE, and a DOCTYPE is the
//     vehicle for XML entity-expansion ("billion laughs") attacks, so it is
//     rejected outright — belt-and-suspenders with xmile's entity-expansion cap.
//
// (Deep nesting is guarded by xmile itself; see xmile ADR 0009.)

import (
	"fmt"
	"strings"
)

// MaxInputBytes is the largest sitemap source Parse/Process/Lint will accept. It
// equals the protocol's 50 MiB per-file maximum (MaxBytes): this is the hard
// resource boundary, distinct from the soft per-entry conformance limits that
// Conformance reports for a document that does parse.
const MaxInputBytes = MaxBytes

// InputError is returned when source is refused before parsing — too large, or
// carrying a DOCTYPE. It is a hard rejection, never a conformance warning.
type InputError struct{ Msg string }

func (e *InputError) Error() string { return e.Msg }

// guardSource enforces the input boundary. A nil return means the source is safe
// to hand to the parser.
func guardSource(src string) error {
	if len(src) > MaxInputBytes {
		return &InputError{Msg: fmt.Sprintf("sitemap source is %d bytes, exceeds the %d-byte (50 MiB) limit", len(src), MaxInputBytes)}
	}
	if hasDoctype(src) {
		return &InputError{Msg: "sitemap must not contain a DOCTYPE (rejected to prevent XML entity-expansion attacks)"}
	}
	return nil
}

// hasDoctype reports whether src's prolog contains a DOCTYPE declaration. A
// DOCTYPE, if present, precedes the root element, after only the XML declaration,
// other processing instructions, comments, and whitespace — which this scan skips.
func hasDoctype(src string) bool {
	for i, n := 0, len(src); i < n; {
		switch c := src[i]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case strings.HasPrefix(src[i:], "<!--"):
			j := strings.Index(src[i+4:], "-->")
			if j < 0 {
				return false // unterminated; the parser will reject it
			}
			i += 4 + j + 3
		case strings.HasPrefix(src[i:], "<?"):
			j := strings.Index(src[i+2:], "?>")
			if j < 0 {
				return false
			}
			i += 2 + j + 2
		case strings.HasPrefix(src[i:], "<!DOCTYPE"):
			return true
		default:
			// The first non-whitespace, non-PI, non-comment, non-DOCTYPE token is
			// the root element (or malformed content): no DOCTYPE in the prolog.
			return false
		}
	}
	return false
}
