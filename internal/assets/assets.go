package assets

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed icon.png
var iconPNG []byte

func AppIcon() fyne.Resource {
	return fyne.NewStaticResource("printer-bridge.png", iconPNG)
}
