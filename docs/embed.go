// Package docs ships the local API guide with the AP executable.
package docs

import "embed"

//go:embed api.md
var Files embed.FS
