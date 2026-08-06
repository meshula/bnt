package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLongestCommonPrefix(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"foo"}, "foo"},
		{[]string{"foo", "foobar"}, "foo"},
		{[]string{"foobar", "foobaz"}, "fooba"},
		{[]string{"alpha", "beta"}, ""},
	}
	for _, c := range cases {
		got := longestCommonPrefix(c.in)
		if got != c.want {
			t.Errorf("LCP(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPathComplete(t *testing.T) {
	tmp := t.TempDir()
	for _, name := range []string{"alpha", "alphabet", "beta"} {
		if err := os.Mkdir(filepath.Join(tmp, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("LCPCompletes", func(t *testing.T) {
		got, matches := pathComplete(tmp + "/al")
		want := tmp + "/alpha"
		if got != want {
			t.Errorf("replacement = %q, want %q", got, want)
		}
		if len(matches) != 2 {
			t.Errorf("expected 2 matches, got %v", matches)
		}
	})

	t.Run("UniqueMatchAddsTrailingSlash", func(t *testing.T) {
		got, matches := pathComplete(tmp + "/be")
		want := tmp + "/beta/"
		if got != want {
			t.Errorf("replacement = %q, want %q", got, want)
		}
		if len(matches) != 1 {
			t.Errorf("expected 1 match, got %v", matches)
		}
	})

	t.Run("NoMatch", func(t *testing.T) {
		got, matches := pathComplete(tmp + "/zzz")
		if got != tmp+"/zzz" {
			t.Errorf("replacement should be unchanged on no match: %q", got)
		}
		if matches != nil {
			t.Errorf("expected no matches, got %v", matches)
		}
	})

	t.Run("TildePreserved", func(t *testing.T) {
		// We can't predict $HOME's contents, but we can at least verify the
		// returned replacement still starts with ~/ when input does.
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			t.Skip("no home dir")
		}
		got, _ := pathComplete("~/")
		if !strings.HasPrefix(got, "~/") && got != "~/" {
			t.Errorf("expected ~/ preserved, got %q", got)
		}
	})

	t.Run("TrailingSlashListsContents", func(t *testing.T) {
		// Reproduces the bug where pressing Tab on `~/local-usd/` (or any
		// path with a trailing slash) was completing back to the parent
		// component because filepath.Join stripped the trailing slash and
		// the function then treated the directory's basename as a partial.
		got, matches := pathComplete(tmp + "/")
		if len(matches) != 3 {
			t.Errorf("expected 3 matches inside tmp, got %v", matches)
		}
		// LCP of alpha/alphabet/beta is "" so the replacement should equal
		// the original input — Tab here just lists candidates, it does not
		// reach back up and complete to the directory's own name.
		if got != tmp+"/" {
			t.Errorf("replacement should stay %q, got %q", tmp+"/", got)
		}
	})
}
