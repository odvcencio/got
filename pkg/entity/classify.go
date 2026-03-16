package entity

// DataFormatSizeThreshold is the maximum size (in bytes) at which data format
// files are still entity-extracted. Above this, they are skipped to avoid
// enormous ASTs from multi-MB data dumps.
const DataFormatSizeThreshold int64 = 256 * 1024 // 256 KB

// dataFormats is the denylist of pure data serialization grammars.
// These produce ASTs with no structural entities (no declarations, no imports).
// Canonical lowercase grammar names from gotreesitter.
var dataFormats = map[string]bool{
	"json":  true, // also covers .jsonl (maps to json grammar)
	"json5": true,
	"yaml":  true,
	"toml":  true,
	"ini":   true,
	"csv":   true,
}

// IsDataFormat reports whether langName is a pure data serialization format.
func IsDataFormat(langName string) bool {
	return dataFormats[langName]
}

// ShouldSkipExtraction reports whether entity extraction should be skipped
// for a file with the given language, size, and force flag.
// Code files are never skipped. Data format files are skipped when above
// the size threshold unless force is true.
func ShouldSkipExtraction(langName string, sizeBytes int64, forceEntities bool) bool {
	if !IsDataFormat(langName) {
		return false
	}
	if forceEntities {
		return false
	}
	return sizeBytes > DataFormatSizeThreshold
}
