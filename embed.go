// Package gohome provides the embedded static web assets.
package gohome

import "embed"

//go:embed web/static
var WebStatic embed.FS
