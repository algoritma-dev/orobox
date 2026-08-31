package config

import "strings"

// SeedDir is where a published runtime image carries a dump of an OroCommerce that was already
// installed, at image build time, for exactly that image's Oro version.
//
// Restoring that dump and reconciling it with oro:platform:update replaces an oro:install, which
// is the single most expensive step there is: on the e2e matrix it costs about five minutes in
// `init`, six in the QA cache and six more in the functional test database, every time a cache
// starts empty — which on an ephemeral CI runner is every run.
//
// The path is a container path. Nothing on the host holds it, and an image built without a seed
// (a local `make docker-image`, or a CI build whose bake step failed) simply has no file there.
// Every consumer treats a missing file as "install from scratch", so the seed is an optimization
// that can be absent, never a dependency.
const SeedDir = "/opt/orobox/seed"

// SeedDumpPath names the dump after the PostgreSQL major that wrote it.
//
// A dump is only restorable by the version pair that produced it: pg_dump writes the settings its
// own server understands, so a dump from 17 restored into 16 dies on `unrecognized configuration
// parameter "transaction_timeout"`, a setting that only exists from 17 on. Putting the major in
// the file name turns that mismatch into a missing file, which every consumer already handles,
// instead of a restore that fails halfway through.
func SeedDumpPath(postgresMajor string) string {
	return SeedDir + "/dump-pg" + postgresMajor + ".sql.gz"
}

// SeedTestDumpPath names the dump of an OroCommerce installed in the test environment, for the
// PostgreSQL major that wrote it.
//
// It is a second dump rather than a replacement for SeedDumpPath because the two have different
// consumers and must not be swapped: the entrypoint seeds a developer's dev or prod install from
// the prod dump, and giving it a test install would hand every new environment the test
// framework's tables and fixture data. The pipeline's test database wants the opposite.
//
// What the separate dump buys is the migrations it does not have to run. A test install already
// holds the schema, the entity config and the generated state that the Tests/Functional/Environment
// migrations produce — and those carry no version, so oro:platform:update replays them on every
// run against a dump that lacks them. Restoring a test dump means the ladder only has to
// regenerate the extend entity classes, which is both faster and free of that replay.
func SeedTestDumpPath(postgresMajor string) string {
	return SeedDir + "/dump-test-pg" + postgresMajor + ".sql.gz"
}

// PostgresMajor is the major version of a pinned image tag: "16.1-alpine" is 16.
func PostgresMajor(version string) string {
	if idx := strings.IndexAny(version, ".-"); idx >= 0 {
		return version[:idx]
	}
	return version
}
