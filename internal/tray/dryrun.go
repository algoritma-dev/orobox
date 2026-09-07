package tray

import "strings"

// RenderDryRun prints menu as an indented text tree, so its construction is inspectable
// without a display or a D-Bus session (orobox-tray --dry-run).
func RenderDryRun(menu Menu) string {
	var b strings.Builder
	for _, item := range menu.Items {
		renderItem(&b, item, 0)
	}
	return b.String()
}

func renderItem(b *strings.Builder, item MenuItem, depth int) {
	b.WriteString(strings.Repeat("  ", depth))
	b.WriteString(item.Label)
	if item.Disabled {
		b.WriteString(" [disabled]")
	}
	if item.Tooltip != "" {
		b.WriteString(" — ")
		b.WriteString(item.Tooltip)
	}
	b.WriteString("\n")
	for _, child := range item.Children {
		renderItem(b, child, depth+1)
	}
}
