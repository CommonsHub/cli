package lumastripe

import "path/filepath"

const (
	Name          = "luma-stripe"
	EventURLsFile = "event-urls.json"
)

func RelPath(elems ...string) string {
	parts := append([]string{"plugins", Name}, elems...)
	return filepath.Join(parts...)
}

func Path(dataDir, year, month string, elems ...string) string {
	parts := []string{dataDir, year, month, RelPath(elems...)}
	return filepath.Join(parts...)
}
