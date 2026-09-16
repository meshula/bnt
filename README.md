# bnt — build-and-test for CMake

## OVERVIEW

`bnt` is a small tool that wraps `cmake --build` and `ctest`. 

To use it, run from a CMake source directory - there's a number of command line switches to run directly, or run without parameters to use it as an interactive tool.

Currently, `bnt` is a bit MacOS specific; it repairs rpaths and signing to get past typical MacOS command line stumbling blocks encountered on complex projects like OpenUSD.

## PROVENANCE

Unless locally overridden, the following Provenance Node applies to all files
```
╭──────────────────────────────────────────────────────────────────────────╮
│ PROVENANCE NODE 000-1                                                    │
│ SCOPE  : https://github.com/meshula/bnt                                  │
├───────────────────────────────────┬──────────────────────────────────────┤
│ PARENT : NONE                     │ SCHEMA : V-1-0                       │
├───────────────────────────────────┼──────────────────────────────────────┤
│ AUTH_ID  : NICK PORCINO           │ DATE        : 2026-08-06             │
│ SRC_AUTH : HUMAN_IN_LOOP          │ TRANSFORMER : QWEN3.6 27B-A3B        │
├───────────────────────────────────┼──────────────────────────────────────┤
│ INDEX_ALLOW : YES                 │ CORP_TRAIN : NO                      │
│ DERIV_ALLOW : YES                 │ GOV_SPDX   : BSD-2-CLAUSE            │
├───────────────────────────────────┴──────────────────────────────────────┤
│ HASH   : UNSIGNED                                                        │
╰──────────────────────────────────────────────────────────────────────────╯
```

## Install

Build in place:

```sh
cd ~/dev/bnt
go build -o bnt .
```

There's a convenient makefile to build and install bnt onto your `PATH`:

```sh
make install                 # -> /usr/local/bin/bnt
make install PREFIX=~/.local # -> ~/.local/bin/bnt
sudo make install            # if /usr/local needs root
```

`make install` respects the standard `PREFIX` (default `/usr/local`) and `DESTDIR`
variables, resulting in installing the binary to `$(DESTDIR)$(PREFIX)/bin/bnt`. 

Other targets:
`make` (build), `make test`, `make vet`, `make uninstall`, `make clean`.

## Usage

```
bnt [flags]
```

With no `--build`/`--test` flags, `bnt` launches interactively. The interactive mode has interactive help, and is meant to be the primary usage mode.

Headless mode is useful for scripting though - using the `--build` flag and/or the `--test` flag, `bnt` runs headless and exits with the underlying tool's status.

### Flags

| flag | description |
|---|---|
| `--source DIR` | source dir, default cwd |
| `--build-dir DIR` | CMake build dir (required headless) |
| `--install-dir DIR` | optional `cmake --install` destination |
| `--build TARGET` | build target (repeatable) |
| `--test REGEX` | ctest regex (repeatable; joined as alternation) |
| `--jobs N` | parallel jobs, default `nproc` |
| `--fix-rpath` | macOS: swap Python.framework rpath on test binaries |
| `--rpath-old PATH` | rpath to remove (default `/Library/Frameworks/Python.framework`) |
| `--rpath-new PATH` | rpath to add (default `/Library/ManagedFrameworks/Python/Python3.framework`) |
| `--test-bin-dir DIR` | dir holding test binaries to patch (default `<build-dir>/../tests`) |
| `--verbose` | `ctest -V` |

### Example: running bnt to build and test OpenUSD's usdProfiles module:

```sh
cd ~/dev/OpenUSD
bnt --build-dir ~/local-usd/build/OpenUSD \
    --install-dir ~/local-usd/build/OpenUSD/pxr/usd/usdProfiles \
    --build usdProfiles --build _usdProfiles \
    --build testUsdProfilesRegistry \
    --build testUsdProfilesProfiles --build testUsdProfilesClaimsAPI \
    --test testUsdProfiles --fix-rpath --verbose
```

## Interactive Tool

### Remembering the last run

The interactive flow remembers the last build/test configuration in a
`.bnt.json` dot file in the current directory (headless runs never write it).
On startup, if that file exists, `bnt` prints a short summary and offers to
replay it. At start, you will see:

```
bnt - build and test

Source directory (CMakelists.txt)
> /path/to/cwd
```
And a prompt line (. means use cwd, ? opens a file browser, etc.)

Hitting enter prompts for the CMake build directory, where you build exists.

`bnt` then introspects the build for all the available build and test targets.

This list is scrollable but for a project like OpenUSD, it will be very long.

```
Select targets and tests - targets 0/656  tests 3/1100

press / to filter
build dir: /path/to/OpenUSD/build

Build targets
> [ ] TestArAdvancedAPI
  [ ] TestArOptinalImplementation
...
Tests
  [ ] ...
```

Hit `/` to invoke a filter to bring the list down to the elements you wish to work with.
For example, typing `/hioOpenEXR` yields:

```
Select targets and tests - targets 0/656  tests 3/1100

press / to filter
build dir: /path/to/OpenUSD/build

Build targets
> [ ] hioOpenEXR
  [ ] hioOpenEXR_headerfiles
  [ ] testHioOpenEXR

Tests
  [ ] testHioOpenEXR
[1-4 / 4]

type to filter * enter: apply * esc: clear
```

Hit enter, then use the space bar and arrows to select targets and tests.

At this point you may build your selection with `b`, or run your selected tests with `t`. bnt will run cmake, install, and ctest as necessary, and present the results. If there were errors, `r` will re-run failed tests in verbose mode, and `v` will force rerunning tests verbosely.

If all is well, you will see:

```
Results
         TESTS PASSED  100% tests passed, 0 tests failed out of 4
...
```

And a detailed list of tests run and timing.

---

This work is copyright 2026 by Nick Porcino, and licensed as BSD-2-Clause.



