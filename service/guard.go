package sitemap

// guard.go — the input boundary. proto-sitemap parses sitemaps fetched from
// arbitrary servers, so it refuses three classes of input before handing bytes
// to xmile, independent of (and in depth over) xmile's own parser guards:
//
//   - Oversized source. The protocol caps a file at 50 MiB uncompressed; nothing
//     conformant is larger, and refusing larger input up front bounds the memory
//     an untrusted document can force the parser to allocate.
//   - Any DOCTYPE. Sitemaps have no legitimate DOCTYPE, and a DOCTYPE is the
//     vehicle for XML entity-expansion ("billion laughs") attacks, so it is
//     rejected outright.
//   - Deep element nesting. A recursive-descent parse of N nested elements costs
//     N stack frames, so unbounded nesting is a stack-exhaustion vector on
//     untrusted input.
//
// Nesting was previously documented as xmile's responsibility, citing an "xmile
// ADR 0009 (entity-expansion + nesting-depth caps)". That ADR does not exist
// upstream (xmile's decisions end at 0008) and xmile carries no depth cap, so
// TestBoundary_RejectsDeepNesting failed as soon as the sibling checkout moved
// forward. The boundary is enforced here, where the rest of it already lives.

import (
	"fmt"
	"strings"
)

// MaxInputBytes is the largest sitemap source Parse/Process/Lint will accept. It
// equals the protocol's 50 MiB per-file maximum (MaxBytes): this is the hard
// resource boundary, distinct from the soft per-entry conformance limits that
// Conformance reports for a document that does parse.
const MaxInputBytes = MaxBytes

// MaxDepth is the deepest element nesting Parse/Process/Lint will accept.
// Sitemaps are shallow by construction: the core is three levels
// (urlset > url > loc), and Google's richest extension — <video:video> with its
// child elements, or <xhtml:link> — reaches five. 100 is two orders of magnitude
// of headroom over anything a real generator emits while still bounding parser
// stack depth far below exhaustion.
const MaxDepth = 100

// InputError is returned when source is refused before parsing — too large,
// carrying a DOCTYPE, or nested past MaxDepth. It is a hard rejection, never a
// conformance warning.
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
	if exceedsDepth(src, MaxDepth) {
		return &InputError{Msg: fmt.Sprintf("sitemap nests elements deeper than the %d-level limit (rejected to prevent parser stack exhaustion)", MaxDepth)}
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

// exceedsDepth reports whether src nests elements more than max levels deep.
//
// It is a pre-parse scan, so it is deliberately structural rather than a second
// XML parser: it tracks only element open/close, skipping the constructs that may
// legitimately contain '<' or '>' (comments, CDATA, processing instructions,
// declarations, and quoted attribute values). An unterminated construct stops the
// scan and passes the source through — the parser proper rejects it moments later.
// Malformed nesting can drive the counter negative; that is harmless here, since
// this guard's only job is to refuse pathological depth before it reaches a
// recursive descent.
func exceedsDepth(src string, max int) bool {
	depth := 0
	for i, n := 0, len(src); i < n; {
		if src[i] != '<' {
			i++
			continue
		}
		switch {
		case strings.HasPrefix(src[i:], "<!--"):
			j := strings.Index(src[i+4:], "-->")
			if j < 0 {
				return false
			}
			i += 4 + j + 3
		case strings.HasPrefix(src[i:], "<![CDATA["):
			j := strings.Index(src[i+9:], "]]>")
			if j < 0 {
				return false
			}
			i += 9 + j + 3
		case strings.HasPrefix(src[i:], "<?"):
			j := strings.Index(src[i+2:], "?>")
			if j < 0 {
				return false
			}
			i += 2 + j + 2
		case strings.HasPrefix(src[i:], "</"):
			depth--
			i = scanTag(src, i+2)
		case strings.HasPrefix(src[i:], "<!"):
			// A declaration; DOCTYPE is already refused above.
			i = scanTag(src, i+2)
		default:
			end, selfClosing := scanStartTag(src, i+1)
			if !selfClosing {
				depth++
				if depth > max {
					return true
				}
			}
			i = end
		}
	}
	return false
}

// scanStartTag advances past a start tag whose '<' was at i-1, honouring quoted
// attribute values (which may contain '>'), and reports whether it self-closed.
func scanStartTag(src string, i int) (end int, selfClosing bool) {
	n := len(src)
	var quote byte
	for i < n {
		c := src[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
			i++
		case c == '"' || c == '\'':
			quote = c
			i++
		case c == '>':
			j := i - 1
			for j >= 0 && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
				j--
			}
			return i + 1, j >= 0 && src[j] == '/'
		default:
			i++
		}
	}
	return n, false
}

// scanTag advances past a tag that carries no self-closing distinction (an end
// tag or a declaration), honouring quoted values.
func scanTag(src string, i int) int {
	end, _ := scanStartTag(src, i)
	return end
}
