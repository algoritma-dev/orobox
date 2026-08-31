package pipeline

import "testing"

func TestHTTPSCloneURL(t *testing.T) {
	cases := []struct {
		name       string
		repository string
		want       string
		wantOK     bool
	}{
		{"scp style", "git@gitlab.algoritma.it:offel/b2b-2026.git", "https://gitlab.algoritma.it/offel/b2b-2026.git", true},
		{"scp style with a leading slash", "git@github.com:/acme/shop.git", "https://github.com/acme/shop.git", true},
		{"ssh scheme", "ssh://git@gitlab.algoritma.it/offel/b2b-2026.git", "https://gitlab.algoritma.it/offel/b2b-2026.git", true},
		{"ssh scheme with a port", "ssh://git@gitlab.algoritma.it:2222/offel/b2b-2026.git", "https://gitlab.algoritma.it/offel/b2b-2026.git", true},
		{"surrounding whitespace", "  git@github.com:acme/shop.git\n", "https://github.com/acme/shop.git", true},
		{"already https", "https://gitlab.algoritma.it/offel/b2b-2026.git", "", false},
		{"local path", "/srv/git/shop.git", "", false},
		{"empty", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := httpsCloneURL(tc.repository)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("url = %q, want %q", got, tc.want)
			}
		})
	}
}
