// Package trayassets embeds the tray's icon PNGs. It exists only so go:embed can reference
// files in the same directory: embed patterns cannot cross into a parent directory, and these
// assets live at the top-level assets/tray/ path packaging (nfpm, the .desktop file) expects.
package trayassets

import _ "embed"

// Icon PNGs, one per orobox-tray state plus a neutral base icon. Placeholders: a real design
// pass can replace the files here without touching any importer.
var (
	//go:embed orobox-symbolic.png
	IconBase []byte

	//go:embed orobox-up-symbolic.png
	IconUp []byte

	//go:embed orobox-down-symbolic.png
	IconDown []byte

	//go:embed orobox-partial-symbolic.png
	IconPartial []byte

	//go:embed orobox-error-symbolic.png
	IconError []byte
)
