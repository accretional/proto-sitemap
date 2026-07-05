module github.com/accretional/proto-sitemap

go 1.26

// proto-sitemap rides xmile's XML engine and gluon's schema compiler. Both are
// local-module dependencies pinned via `replace => ../<dep>`; xmile's own
// `replace`s do not carry over transitively, so gluon and proto-merge are
// repeated here (a clean checkout needs all three checked out as siblings —
// setup.sh does this).
replace github.com/accretional/xmile => ../xmile

replace github.com/accretional/gluon => ../gluon

replace github.com/accretional/merge => ../proto-merge

require (
	github.com/accretional/xmile v0.0.0-00010101000000-000000000000
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/accretional/gluon v0.0.0-00010101000000-000000000000 // indirect
	github.com/accretional/proto-expr v0.0.0-20260416071217-9a69001c59bb // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260120221211-b8f7ae30c516 // indirect
	google.golang.org/grpc v1.80.0 // indirect
)
