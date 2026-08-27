package config

import "testing"

func TestPostgresMajorFromAPinnedTag(t *testing.T) {
	for tag, want := range map[string]string{
		"17.6-alpine": "17",
		"16.1-alpine": "16",
		"16":          "16",
		"18-alpine":   "18",
	} {
		if got := PostgresMajor(tag); got != want {
			t.Errorf("PostgresMajor(%q) = %q, want %q", tag, got, want)
		}
	}
}

// A dump is only restorable by the version pair that wrote it, so the major has to be in the file
// name: that is what makes an image whose seed was dumped by another server look like an image
// with no seed, which every consumer already handles, rather than a restore that fails halfway.
func TestSeedDumpPathCarriesThePostgresMajor(t *testing.T) {
	seventeen := SeedDumpPath(PostgresMajor("17.6-alpine"))
	sixteen := SeedDumpPath(PostgresMajor("16.1-alpine"))

	if seventeen == sixteen {
		t.Fatalf("two Postgres majors share the seed path %q", seventeen)
	}
	if want := SeedDir + "/dump-pg17.sql.gz"; seventeen != want {
		t.Errorf("SeedDumpPath = %q, want %q", seventeen, want)
	}
}
