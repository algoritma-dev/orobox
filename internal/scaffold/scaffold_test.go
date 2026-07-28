package scaffold

import "testing"

func TestParseBundleArg(t *testing.T) {
	tests := []struct {
		name          string
		arg           string
		namespaceFlag string
		packageFlag   string
		want          BundleOptions
		wantErr       bool
	}{
		{
			name: "fully qualified class",
			arg:  `Acme\FooBundle\AcmeFooBundle`,
			want: BundleOptions{
				ClassName:   "AcmeFooBundle",
				Namespace:   `Acme\FooBundle`,
				Prefix:      "AcmeFoo",
				Alias:       "acme_foo",
				PackageName: "acme/foo-bundle",
			},
		},
		{
			name:          "short name with namespace flag",
			arg:           "AcmeFooBundle",
			namespaceFlag: `Acme\FooBundle`,
			want: BundleOptions{
				ClassName:   "AcmeFooBundle",
				Namespace:   `Acme\FooBundle`,
				Prefix:      "AcmeFoo",
				Alias:       "acme_foo",
				PackageName: "acme/foo-bundle",
			},
		},
		{
			name: "short name, no flags falls back to top-level namespace and orobox package",
			arg:  "AcmeFooBundle",
			want: BundleOptions{
				ClassName:   "AcmeFooBundle",
				Namespace:   "AcmeFooBundle",
				Prefix:      "AcmeFoo",
				Alias:       "acme_foo",
				PackageName: "orobox/acme-foo-bundle",
			},
		},
		{
			name:        "package flag overrides derived package",
			arg:         `Acme\FooBundle\AcmeFooBundle`,
			packageFlag: "custom/pkg",
			want: BundleOptions{
				ClassName:   "AcmeFooBundle",
				Namespace:   `Acme\FooBundle`,
				Prefix:      "AcmeFoo",
				Alias:       "acme_foo",
				PackageName: "custom/pkg",
			},
		},
		{
			name:    "empty arg errors",
			arg:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBundleArg(tt.arg, tt.namespaceFlag, tt.packageFlag)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseBundleArg(%q, %q, %q):\n got  %+v\n want %+v", tt.arg, tt.namespaceFlag, tt.packageFlag, got, tt.want)
			}
		})
	}
}
