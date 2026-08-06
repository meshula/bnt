package cmake

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

var phonyTargets = map[string]bool{
	"all":                     true,
	"clean":                   true,
	"depend":                  true,
	"edit_cache":              true,
	"rebuild_cache":           true,
	"install":                 true,
	"install/local":           true,
	"install/strip":           true,
	"list_install_components": true,
	"package":                 true,
	"package_source":          true,
	"test":                    true,
	"help":                    true,
}

// Suffixes used by CMake helper / aggregate targets that almost no one wants
// to build directly. Filtered out of the discovered list to keep the UI sane
// on big projects (OpenUSD generates ~640 targets, most of which are these).
var phonySuffixes = []string{
	"_pythonfiles",
	"_resources",
	"_plugInfo",
	"_pythonImports",
}

// IsBuildDir reports whether dir contains a CMakeCache.txt.
func IsBuildDir(dir string) bool {
	if dir == "" {
		return false
	}
	st, err := os.Stat(filepath.Join(dir, "CMakeCache.txt"))
	return err == nil && !st.IsDir()
}

// DiscoverTargets queries the build system's help target and returns user
// targets, filtering out cmake's phony bookkeeping targets.
func DiscoverTargets(buildDir string) ([]string, error) {
	cmd := exec.Command("cmake", "--build", buildDir, "--target", "help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("cmake --build help failed: %v\n%s", err, string(out))
	}
	// Lines look like:
	//   ... usdProfiles
	//   ... testUsdProfilesRegistry
	re := regexp.MustCompile(`^\.\.\.\s+(\S+)`)
	seen := map[string]bool{}
	var targets []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		m := re.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		t := m[1]
		if phonyTargets[t] {
			continue
		}
		// Filter object-file targets (paths and *.o entries).
		if strings.ContainsAny(t, "/\\") || strings.HasSuffix(t, ".o") {
			continue
		}
		phony := false
		for _, s := range phonySuffixes {
			if strings.HasSuffix(t, s) {
				phony = true
				break
			}
		}
		if phony {
			continue
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		targets = append(targets, t)
	}
	sort.Strings(targets)
	return targets, nil
}

// DiscoverTests runs `ctest -N` in buildDir and returns the test names.
func DiscoverTests(buildDir string) ([]string, error) {
	cmd := exec.Command("ctest", "-N")
	cmd.Dir = buildDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ctest -N failed: %v\n%s", err, string(out))
	}
	re := regexp.MustCompile(`^\s*Test\s+#\d+:\s+(\S+)`)
	var tests []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		m := re.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		tests = append(tests, m[1])
	}
	sort.Strings(tests)
	return tests, nil
}

// Build runs `cmake --build buildDir --target ... -j N`, streaming output to w.
func Build(buildDir string, targets []string, jobs int, w io.Writer) error {
	if len(targets) == 0 {
		return fmt.Errorf("no targets to build")
	}
	args := []string{"--build", buildDir, "--target"}
	args = append(args, targets...)
	if jobs > 0 {
		args = append(args, "--parallel", fmt.Sprintf("%d", jobs))
	}
	cmd := exec.Command("cmake", args...)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

// Install runs `cmake --install dir [--prefix prefix]`.
//
// dir is the build directory (or a subdir of it) whose cmake_install.cmake
// describes what to install — this is the bnt.sh-style component install.
// prefix, if non-empty, is passed as `--prefix` to override the install
// destination set at configure time.
func Install(dir, prefix string, w io.Writer) error {
	args := []string{"--install", dir}
	if prefix != "" {
		args = append(args, "--prefix", prefix)
	}
	cmd := exec.Command("cmake", args...)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

// FixRPath swaps oldRPath for newRPath on bin (macOS only). Failures are
// tolerated (matching bnt.sh's `|| true`); a nil error is returned.
func FixRPath(bin, oldRPath, newRPath string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	_ = exec.Command("install_name_tool", "-delete_rpath", oldRPath, bin).Run()
	_ = exec.Command("install_name_tool", "-add_rpath", newRPath, bin).Run()
	return nil
}

// RunTests execs ctest with inherited stdio. Returns ctest's exit error.
func RunTests(buildDir, regex string, verbose bool) error {
	args := []string{}
	if regex != "" {
		args = append(args, "-R", regex)
	}
	if verbose {
		args = append(args, "-V")
	}
	cmd := exec.Command("ctest", args...)
	cmd.Dir = buildDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
