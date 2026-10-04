// Package web embeds the built interface into the AP executable.
// Run make web before building a release binary.
package web

import "embed"

// Files contains the frontend build, including its static assets.
//
//go:embed all:dist
var Files embed.FS
