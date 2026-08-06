package main

import "testing"

func TestResolveInstall(t *testing.T) {
	build := "/Users/me/local-usd/build/OpenUSD"
	cases := []struct {
		name       string
		user       string
		wantDir    string
		wantPrefix string
	}{
		{
			name:       "Empty",
			user:       "",
			wantDir:    build,
			wantPrefix: "",
		},
		{
			name:       "RelativeSubdir",
			user:       "pxr/usd/usdProfiles",
			wantDir:    build + "/pxr/usd/usdProfiles",
			wantPrefix: "",
		},
		{
			name:       "AbsoluteUnderBuild",
			user:       build + "/pxr/usd/usdProfiles",
			wantDir:    build + "/pxr/usd/usdProfiles",
			wantPrefix: "",
		},
		{
			name:       "AbsoluteOutsideBuildBecomesPrefix",
			user:       "/Users/me/local-usd",
			wantDir:    build,
			wantPrefix: "/Users/me/local-usd",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotDir, gotPrefix := resolveInstall(build, c.user)
			if gotDir != c.wantDir || gotPrefix != c.wantPrefix {
				t.Errorf("resolveInstall(%q, %q) = (%q, %q), want (%q, %q)",
					build, c.user, gotDir, gotPrefix, c.wantDir, c.wantPrefix)
			}
		})
	}
}
