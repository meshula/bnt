// Package report renders a focused error report from buffered command
// output. The contract: keep the head and tail of the log, plus a window
// around every line that mentions error/fatal/failure. Merge overlapping
// windows, separate the surviving spans with "...", and highlight error
// keywords in red and warnings in yellow on terminals that support it.
package report

import (
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	contextRadius = 20
	headLines     = 25
	tailLines     = 25
)

var (
	errorRE   = regexp.MustCompile(`(?i)error|fatal|failure`)
	warningRE = regexp.MustCompile(`(?i)warning`)

	redStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	yellowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
)

type span struct{ start, end int } // both inclusive, 0-based line indexes

// Render returns a focused, highlighted excerpt of out. If out is empty or
// nothing matches, an empty string is returned.
func Render(out string) string {
	if strings.TrimSpace(out) == "" {
		return ""
	}
	// Strip a single trailing newline so we don't render a blank tail line.
	out = strings.TrimRight(out, "\n")
	lines := strings.Split(out, "\n")
	n := len(lines)
	if n == 0 {
		return ""
	}

	var spans []span

	// Windows around every error/fatal/failure line.
	for i, ln := range lines {
		if errorRE.MatchString(ln) {
			spans = append(spans, span{i - contextRadius, i + contextRadius})
		}
	}

	// Always include head and tail.
	if headLines > 0 {
		spans = append(spans, span{0, headLines - 1})
	}
	if tailLines > 0 {
		spans = append(spans, span{n - tailLines, n - 1})
	}

	// Clamp to valid range.
	for i := range spans {
		if spans[i].start < 0 {
			spans[i].start = 0
		}
		if spans[i].end > n-1 {
			spans[i].end = n - 1
		}
	}

	merged := mergeSpans(spans)

	var sb strings.Builder
	for i, sp := range merged {
		if i > 0 {
			sb.WriteString("\n...\n\n")
		}
		for j := sp.start; j <= sp.end; j++ {
			sb.WriteString(highlight(lines[j]))
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// mergeSpans sorts spans by start, then collapses adjacent/overlapping ones.
// Spans that touch (gap of 0 or 1 line) are merged so we don't print "..."
// between two effectively-contiguous excerpts.
func mergeSpans(in []span) []span {
	if len(in) == 0 {
		return nil
	}
	sort.Slice(in, func(i, j int) bool { return in[i].start < in[j].start })
	out := []span{in[0]}
	for _, s := range in[1:] {
		last := &out[len(out)-1]
		if s.start <= last.end+1 {
			if s.end > last.end {
				last.end = s.end
			}
		} else {
			out = append(out, s)
		}
	}
	return out
}

// highlight wraps any "error"/"fatal"/"failure" tokens in red and "warning"
// in yellow, preserving the original casing and surrounding text.
func highlight(line string) string {
	line = errorRE.ReplaceAllStringFunc(line, func(m string) string { return redStyle.Render(m) })
	line = warningRE.ReplaceAllStringFunc(line, func(m string) string { return yellowStyle.Render(m) })
	return line
}
