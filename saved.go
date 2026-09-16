package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// savedName is the dot file bnt writes in the cwd to remember the last
// interactive build/test configuration.
const savedName = ".bnt.json"

// Saved is the interactive configuration remembered between runs. It is written
// only by the interactive flow (headless runs never touch it) and replayed via
// runHeadless when the user accepts the (b/t/n) prompt on startup.
type Saved struct {
	BuildDir   string   `json:"buildDir"`
	InstallDir string   `json:"installDir"`
	Targets    []string `json:"targets"`
	Tests      []string `json:"tests"`
	FixRPath   bool     `json:"fixRPath"`
	Verbose    bool     `json:"verbose"`
	Jobs       int      `json:"jobs"`
}

// savedPath returns the dot-file path for a given working directory.
func savedPath(cwd string) string { return filepath.Join(cwd, savedName) }

// loadSaved reads and unmarshals the dot file in cwd. ok is false if the file
// is missing or corrupt (so a stale/garbage file quietly falls back to the
// interactive flow rather than erroring).
func loadSaved(cwd string) (*Saved, bool) {
	data, err := os.ReadFile(savedPath(cwd))
	if err != nil {
		return nil, false
	}
	var s Saved
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, false
	}
	return &s, true
}

// saveSaved marshals s (indented) and writes it to the dot file in cwd.
func saveSaved(cwd string, s Saved) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(savedPath(cwd), data, 0o644)
}

// summaryCap bounds how many target/test lines summary() prints before it
// collapses the tail into an "…N others" line.
const summaryCap = 16

// summary renders a human-readable description of the saved configuration for
// the startup prompt: the build and install dirs, then a combined list of
// target/test lines capped at summaryCap with an "…N others" tail.
func (s Saved) summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "build dir:   %s\n", s.BuildDir)
	install := s.InstallDir
	if install == "" {
		install = "(none)"
	}
	fmt.Fprintf(&b, "install dir: %s\n", install)

	lines := make([]string, 0, len(s.Targets)+len(s.Tests))
	for _, t := range s.Targets {
		lines = append(lines, "target: "+t)
	}
	for _, t := range s.Tests {
		lines = append(lines, "test:   "+t)
	}

	shown := lines
	if len(lines) > summaryCap {
		shown = lines[:summaryCap]
	}
	for _, l := range shown {
		b.WriteString("  ")
		b.WriteString(l)
		b.WriteString("\n")
	}
	if len(lines) > summaryCap {
		fmt.Fprintf(&b, "  …%d others\n", len(lines)-summaryCap)
	}
	return b.String()
}
