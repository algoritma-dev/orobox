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
// The test dump is a sibling of the prod one, under the same directory the images populate: the
// pipeline reads this exact path out of the runtime image, so a rename here is a rung that
// silently stops firing.
func TestSeedTestDumpPathIsTheTestSiblingOfTheProdDump(t *testing.T) {
	if got, want := SeedTestDumpPath("16"), "/opt/orobox/seed/dump-test-pg16.sql.gz"; got != want {
		t.Errorf("SeedTestDumpPath(16) = %q, want %q", got, want)
	}
	if SeedTestDumpPath("16") == SeedDumpPath("16") {
		t.Error("the test dump and the prod dump must not share a path")
	}
}

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
