package components

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ScriptExtension is the extension of a renderer script file.
//
// The files are sorted by name, so the prefix orders them.
const ScriptExtension = ".pb.js"

// LoadScript reads the renderer script from a directory.
//
// The directory is scanned for the files ending with [ScriptExtension]
// and they are concatenated in name order, so that a project can split
// the script across several files.
//
// A missing directory is not an error: it yields an empty script, which
// means the Go renderer is used.
//
// The script must define a "flintRender" function:
//
//	function flintRender(record) {
//	    return {
//	        title:   record.label || record.id,
//	        summary: record.description || "",
//	        fields:  Object.keys(record.fields || {}).map(k => k + ": " + record[k]),
//	    }
//	}
func LoadScript(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}

		return "", fmt.Errorf("components: read the script directory %q: %w", dir, err)
	}

	names := make([]string, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if !strings.HasSuffix(entry.Name(), ScriptExtension) {
			continue
		}

		names = append(names, entry.Name())
	}

	sort.Strings(names)

	var script strings.Builder

	for _, name := range names {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", fmt.Errorf("components: read the script %q: %w", name, err)
		}

		script.Write(content)
		script.WriteString("\n")
	}

	return script.String(), nil
}
