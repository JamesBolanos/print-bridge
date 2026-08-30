// Package assets embeds application resources.
package assets

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed icon.png
var iconPNG []byte

// AppIcon returns the embedded application icon as a Fyne resource.
func AppIcon() fyne.Resource {
	return fyne.NewStaticResource("printer-bridge.png", iconPNG)
}
