// Package main scans .templ files for responsive CSS classes
// and generates app/_entry.css combining @source inline() with globals.css.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	classRe      = regexp.MustCompile(`class="([^"]*)"`)
	dynRe        = regexp.MustCompile(`class=\{\s*"([^"]*)"`)
	responsiveRe = regexp.MustCompile(`^(sm|md|lg|xl|dark):`)
)

func main() {
	appDir := "app"
	classes := make(map[string]bool)

	err := filepath.Walk(appDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".templ") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		content := string(data)

		for _, m := range classRe.FindAllStringSubmatch(content, -1) {
			extractClasses(m[1], classes)
		}
		for _, m := range dynRe.FindAllStringSubmatch(content, -1) {
			extractClasses(m[1], classes)
		}

		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "walk error: %v\n", err)
		os.Exit(1)
	}

	sorted := make([]string, 0, len(classes))
	for c := range classes {
		sorted = append(sorted, c)
	}
	sort.Strings(sorted)

	// Read globals.css
	globalsPath := filepath.Join(appDir, "globals.css")
	globals, err := os.ReadFile(globalsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read globals.css error: %v\n", err)
		os.Exit(1)
	}

	// Build entry: @source inline + globals.css
	source := strings.Join(sorted, " ")
	entry := fmt.Sprintf("@source inline(%q);\n\n%s", source, string(globals))

	outPath := filepath.Join(appDir, "_entry.css")
	if err := os.WriteFile(outPath, []byte(entry), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated %s with %d responsive classes\n", outPath, len(sorted))
}

func extractClasses(raw string, out map[string]bool) {
	for _, c := range strings.Fields(raw) {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if responsiveRe.MatchString(c) {
			out[c] = true
		}
	}
}
