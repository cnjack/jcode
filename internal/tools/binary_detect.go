package tools

import (
	"path/filepath"
	"strings"
)

var binaryExtensions = map[string]bool{
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true, ".7z": true, ".rar": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true, ".ico": true, ".tiff": true,
	".mp3": true, ".mp4": true, ".avi": true, ".mkv": true, ".wav": true, ".flac": true, ".ogg": true, ".mov": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
	".wasm": true, ".o": true, ".a": true, ".pyc": true, ".class": true,
	".sqlite": true, ".db": true,
	".ttf": true, ".otf": true, ".woff": true, ".woff2": true, ".eot": true,
	".jar": true, ".war": true, ".ear": true,
}

// detectBinaryByExtension returns true if the file extension indicates a binary file.
func detectBinaryByExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return binaryExtensions[ext]
}

// detectBinaryByContent returns true if the content appears to be binary.
// It checks for NULL bytes in the first 8192 bytes.
func detectBinaryByContent(content []byte) bool {
	limit := 8192
	if len(content) < limit {
		limit = len(content)
	}
	for i := 0; i < limit; i++ {
		if content[i] == 0 {
			return true
		}
	}
	return false
}
