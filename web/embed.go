// Package web embeds the static browser frontend (index.html + app.js) so
// it ships inside the Go binary with no separate file server, matching the
// sibling raftkv demo's deployment shape.
package web

import "embed"

//go:embed index.html app.js index.ja.html app.ja.js
var Assets embed.FS
