// Package assets embeds static resource files (icon and logo) into the binary.
package assets

import _ "embed"

//go:embed icon.png
var IconPNG []byte

//go:embed logo.png
var LogoPNG []byte
