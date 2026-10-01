package yamledit

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// fixture mixes head, line and foot comments so every edit test can assert
// that none of them is lost.
const fixture = `# Head comment: orobox project file
version: 1

image: # line comment on image
  # comment above php_extensions
  php_extensions: [redis]
  php_version: "8.4" # keep this pin

services:
  # comment above a
  a:
    image: nginx # line comment on a
# foot comment
`

var fixtureComments = []string{
	"# Head comment: orobox project file",
	"# line comment on image",
	"# comment above php_extensions",
	"# keep this pin",
	"# comment above a",
	"# line comment on a",
	"# foot comment",
}

func mustParse(t *testing.T, src string) *Doc {
	t.Helper()
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return d
}

// roundTrip serializes the doc and decodes it into a generic map so tests can
// assert semantics instead of whitespace.
func roundTrip(t *testing.T, d *Doc) (string, map[string]any) {
	t.Helper()
	out, err := d.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, out)
	}
	return string(out), m
}

func assertComments(t *testing.T, out string) {
	t.Helper()
	for _, c := range fixtureComments {
		if !strings.Contains(out, c) {
			t.Errorf("comment %q lost; output:\n%s", c, out)
		}
	}
}

func fragment(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(src), &n); err != nil {
		t.Fatal(err)
	}
	return n.Content[0]
}

func TestSetScalarKeepsComments(t *testing.T) {
	d := mustParse(t, fixture)
	if err := d.SetScalar([]string{"image", "dockerfile"}, "docker/Dockerfile"); err != nil {
		t.Fatal(err)
	}
	out, m := roundTrip(t, d)
	assertComments(t, out)
	img := m["image"].(map[string]any)
	if img["dockerfile"] != "docker/Dockerfile" {
		t.Errorf("image.dockerfile = %v", img["dockerfile"])
	}
	if img["php_version"] != "8.4" {
		t.Errorf("sibling php_version changed: %v", img["php_version"])
	}
}

func TestSetScalarReplacesExisting(t *testing.T) {
	d := mustParse(t, fixture)
	if err := d.SetScalar([]string{"image", "php_version"}, "8.5"); err != nil {
		t.Fatal(err)
	}
	out, m := roundTrip(t, d)
	assertComments(t, out)
	if got := m["image"].(map[string]any)["php_version"]; got != "8.5" {
		t.Errorf("php_version = %v, want 8.5", got)
	}
}

func TestSetScalarKeepsStringType(t *testing.T) {
	// A value that looks like a bool/int must stay a string, not be re-typed
	// when read back.
	d := mustParse(t, "")
	if err := d.SetScalar([]string{"k"}, "true"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetScalar([]string{"n"}, "8080"); err != nil {
		t.Fatal(err)
	}
	_, m := roundTrip(t, d)
	if m["k"] != "true" || m["n"] != "8080" {
		t.Errorf("values re-typed: %#v", m)
	}
}

func TestSetScalarOverNonScalarFails(t *testing.T) {
	d := mustParse(t, fixture)
	err := d.SetScalar([]string{"image"}, "x")
	if err == nil || !strings.Contains(err.Error(), "image") {
		t.Fatalf("want error naming path, got %v", err)
	}
}

func TestSetScalarThroughNonMappingFails(t *testing.T) {
	d := mustParse(t, fixture)
	err := d.SetScalar([]string{"version", "x"}, "y")
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("want error naming path, got %v", err)
	}
}

func TestSetScalarEmptyPathFails(t *testing.T) {
	if err := mustParse(t, "").SetScalar(nil, "x"); err == nil {
		t.Fatal("want error for empty path")
	}
}

func TestParseEmptyThenSetScalar(t *testing.T) {
	for name, src := range map[string]string{"nil": "", "blank": "  \n\n", "comments": "# just a comment\n"} {
		t.Run(name, func(t *testing.T) {
			d, err := Parse([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if err := d.SetScalar([]string{"image", "dockerfile"}, "docker/Dockerfile"); err != nil {
				t.Fatal(err)
			}
			out, m := roundTrip(t, d)
			if m["image"].(map[string]any)["dockerfile"] != "docker/Dockerfile" {
				t.Errorf("unexpected output:\n%s", out)
			}
			if name == "comments" && !strings.Contains(out, "# just a comment") {
				t.Errorf("comment lost:\n%s", out)
			}
		})
	}
}

func TestParseNilBytes(t *testing.T) {
	d, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Kind(nil); got != yaml.MappingNode {
		t.Errorf("root kind = %v, want mapping", got)
	}
}

func TestParseRejectsNonMappingRoot(t *testing.T) {
	for _, src := range []string{"- a\n- b\n", "just a string\n", "42\n"} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("Parse(%q): want error", src)
		}
	}
}

func TestParseInvalidYAML(t *testing.T) {
	if _, err := Parse([]byte("a: [unclosed\n")); err == nil {
		t.Fatal("want syntax error")
	}
}

func TestAppendUniqueDedupes(t *testing.T) {
	d := mustParse(t, fixture)
	if err := d.AppendUnique([]string{"image", "php_extensions"}, "redis", "redis", "xsl"); err != nil {
		t.Fatal(err)
	}
	out, m := roundTrip(t, d)
	assertComments(t, out)
	got := m["image"].(map[string]any)["php_extensions"].([]any)
	if len(got) != 2 || got[0] != "redis" || got[1] != "xsl" {
		t.Errorf("php_extensions = %v, want [redis xsl]", got)
	}
	// Flow style of the existing sequence is preserved.
	if !strings.Contains(out, "[redis, xsl]") {
		t.Errorf("flow style not preserved:\n%s", out)
	}
}

func TestAppendUniquePreservesBlockStyle(t *testing.T) {
	d := mustParse(t, "list:\n  - a # keep\n")
	if err := d.AppendUnique([]string{"list"}, "b"); err != nil {
		t.Fatal(err)
	}
	out, m := roundTrip(t, d)
	if !strings.Contains(out, "# keep") || strings.Contains(out, "[") {
		t.Errorf("block style/comment not preserved:\n%s", out)
	}
	if got := m["list"].([]any); len(got) != 2 {
		t.Errorf("list = %v", got)
	}
}

func TestAppendUniqueCreatesSequence(t *testing.T) {
	d := mustParse(t, fixture)
	if err := d.AppendUnique([]string{"image", "apt_packages"}, "git", "git", "curl"); err != nil {
		t.Fatal(err)
	}
	out, m := roundTrip(t, d)
	assertComments(t, out)
	got := m["image"].(map[string]any)["apt_packages"].([]any)
	if len(got) != 2 || got[0] != "git" || got[1] != "curl" {
		t.Errorf("apt_packages = %v", got)
	}
}

func TestAppendUniqueCreatesIntermediates(t *testing.T) {
	d := mustParse(t, "")
	if err := d.AppendUnique([]string{"a", "b", "c"}, "x"); err != nil {
		t.Fatal(err)
	}
	if !d.Has([]string{"a", "b", "c"}) || d.Kind([]string{"a", "b", "c"}) != yaml.SequenceNode {
		t.Error("sequence not created at a.b.c")
	}
}

func TestAppendUniqueFillsNullValue(t *testing.T) {
	// `key:` with nothing after it parses as null; it is the natural empty
	// placeholder in a hand-written file, so it should become the sequence.
	d := mustParse(t, "exts: # why\n")
	if err := d.AppendUnique([]string{"exts"}, "redis"); err != nil {
		t.Fatal(err)
	}
	out, m := roundTrip(t, d)
	if got := m["exts"].([]any); len(got) != 1 || got[0] != "redis" {
		t.Errorf("exts = %v", got)
	}
	if !strings.Contains(out, "# why") {
		t.Errorf("comment lost:\n%s", out)
	}
}

func TestAppendUniqueOnNonSequenceFails(t *testing.T) {
	d := mustParse(t, fixture)
	err := d.AppendUnique([]string{"image"}, "x")
	if err == nil || !strings.Contains(err.Error(), "image") {
		t.Fatalf("want error naming path, got %v", err)
	}
	if err := d.AppendUnique([]string{"version"}, "x"); err == nil {
		t.Fatal("want error on scalar")
	}
}

func TestMergeMappingSkipsExisting(t *testing.T) {
	d := mustParse(t, fixture)
	frag := fragment(t, "a:\n  image: other\nb:\n  image: redis\n")
	added, skipped, err := d.MergeMapping([]string{"services"}, frag, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(added, ",") != "b" || strings.Join(skipped, ",") != "a" {
		t.Errorf("added=%v skipped=%v", added, skipped)
	}
	out, m := roundTrip(t, d)
	assertComments(t, out)
	svc := m["services"].(map[string]any)
	if svc["a"].(map[string]any)["image"] != "nginx" {
		t.Errorf("a was modified: %v", svc["a"])
	}
	if svc["b"].(map[string]any)["image"] != "redis" {
		t.Errorf("b not added: %v", svc["b"])
	}
}

func TestMergeMappingOverwrite(t *testing.T) {
	d := mustParse(t, fixture)
	frag := fragment(t, "a:\n  image: other\nb:\n  image: redis\n")
	added, skipped, err := d.MergeMapping([]string{"services"}, frag, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 || strings.Join(added, ",") != "a,b" {
		t.Errorf("added=%v skipped=%v", added, skipped)
	}
	out, m := roundTrip(t, d)
	// The line comment sat on the replaced value of a; every other comment
	// must survive.
	for _, c := range fixtureComments {
		if c == "# line comment on a" {
			continue
		}
		if !strings.Contains(out, c) {
			t.Errorf("comment %q lost:\n%s", c, out)
		}
	}
	if got := m["services"].(map[string]any)["a"].(map[string]any)["image"]; got != "other" {
		t.Errorf("a not replaced: %v", got)
	}
}

func TestMergeMappingCreatesTarget(t *testing.T) {
	d := mustParse(t, "")
	frag := fragment(t, "redis:\n  image: redis:7\n")
	added, skipped, err := d.MergeMapping([]string{"services"}, frag, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != "redis" || len(skipped) != 0 {
		t.Errorf("added=%v skipped=%v", added, skipped)
	}
	_, m := roundTrip(t, d)
	if m["services"].(map[string]any)["redis"].(map[string]any)["image"] != "redis:7" {
		t.Errorf("unexpected: %v", m)
	}
}

func TestMergeMappingIntoEmptyFlowMapping(t *testing.T) {
	d := mustParse(t, "services: {}\n")
	frag := fragment(t, "a:\n  image: x\n")
	if _, _, err := d.MergeMapping([]string{"services"}, frag, false); err != nil {
		t.Fatal(err)
	}
	out, _ := roundTrip(t, d)
	if strings.Contains(out, "{") {
		t.Errorf("expected block style after filling an empty {}:\n%s", out)
	}
}

func TestMergeMappingFragmentOrderAndIsolation(t *testing.T) {
	d := mustParse(t, "")
	frag := fragment(t, "z: 1\na: 2\nm: 3\n")
	added, _, err := d.MergeMapping(nil, frag, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(added, ",") != "z,a,m" {
		t.Errorf("added = %v, want fragment order", added)
	}
	// The doc must hold deep copies: mutating the fragment afterwards must not
	// leak in (recipes reuse one fragment for several files).
	frag.Content[1].Value = "mutated"
	_, m := roundTrip(t, d)
	if m["z"] == "mutated" {
		t.Error("fragment nodes are shared with the document")
	}
}

func TestMergeMappingAcceptsDocumentNode(t *testing.T) {
	var n yaml.Node
	if err := yaml.Unmarshal([]byte("k: v\n"), &n); err != nil {
		t.Fatal(err)
	}
	d := mustParse(t, "")
	added, _, err := d.MergeMapping(nil, &n, false)
	if err != nil || len(added) != 1 {
		t.Fatalf("added=%v err=%v", added, err)
	}
}

func TestMergeMappingErrors(t *testing.T) {
	d := mustParse(t, fixture)
	seq := fragment(t, "- a\n- b\n")
	if _, _, err := d.MergeMapping([]string{"services"}, seq, false); err == nil {
		t.Error("want error for non-mapping fragment")
	}
	if _, _, err := d.MergeMapping([]string{"services"}, nil, false); err == nil {
		t.Error("want error for nil fragment")
	}
	frag := fragment(t, "a: 1\n")
	if _, _, err := d.MergeMapping([]string{"version"}, frag, false); err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("want error naming path for scalar target, got %v", err)
	}
}

func TestHasAndKind(t *testing.T) {
	d := mustParse(t, fixture)
	cases := []struct {
		path []string
		has  bool
		kind yaml.Kind
	}{
		{nil, true, yaml.MappingNode},
		{[]string{"version"}, true, yaml.ScalarNode},
		{[]string{"image"}, true, yaml.MappingNode},
		{[]string{"image", "php_extensions"}, true, yaml.SequenceNode},
		{[]string{"image", "nope"}, false, 0},
		{[]string{"nope", "deeper"}, false, 0},
		{[]string{"version", "deeper"}, false, 0},
	}
	for _, c := range cases {
		if got := d.Has(c.path); got != c.has {
			t.Errorf("Has(%v) = %v, want %v", c.path, got, c.has)
		}
		if got := d.Kind(c.path); got != c.kind {
			t.Errorf("Kind(%v) = %v, want %v", c.path, got, c.kind)
		}
	}
}

func TestReadsFollowAliases(t *testing.T) {
	src := "base: &b\n  image: nginx\nsvc: *b\n"
	d := mustParse(t, src)
	if !d.Has([]string{"svc", "image"}) {
		t.Error("Has should follow alias svc -> base")
	}
	if got := d.Kind([]string{"svc"}); got != yaml.MappingNode {
		t.Errorf("Kind(svc) = %v, want mapping (dereferenced)", got)
	}
}

func TestWritesDoNotGoThroughAliases(t *testing.T) {
	// Editing through an alias would silently change the anchored mapping and
	// every other alias of it.
	src := "base: &b\n  image: nginx\nsvc: *b\n"
	d := mustParse(t, src)
	if err := d.SetScalar([]string{"svc", "image"}, "x"); err == nil {
		t.Fatal("want error writing through an alias")
	}
	_, m := roundTrip(t, d)
	if m["base"].(map[string]any)["image"] != "nginx" {
		t.Error("anchored mapping was modified")
	}
}

func TestMergeKeysAreNotEdited(t *testing.T) {
	// `<<` keys are not the mapping's own keys: a key defined only via merge
	// is added as an explicit own key rather than edited in the source.
	src := "base: &b\n  image: nginx\nsvc:\n  <<: *b\n"
	d := mustParse(t, src)
	if err := d.SetScalar([]string{"svc", "image"}, "caddy"); err != nil {
		t.Fatal(err)
	}
	_, m := roundTrip(t, d)
	if m["base"].(map[string]any)["image"] != "nginx" {
		t.Error("merge source was modified")
	}
	if m["svc"].(map[string]any)["image"] != "caddy" {
		t.Errorf("svc.image = %v", m["svc"])
	}
}

func TestBytesUsesTwoSpaceIndent(t *testing.T) {
	d := mustParse(t, "")
	if err := d.SetScalar([]string{"a", "b", "c"}, "v"); err != nil {
		t.Fatal(err)
	}
	out, _ := roundTrip(t, d)
	if want := "a:\n  b:\n    c: v\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestUntouchedDocumentRoundTrips(t *testing.T) {
	d := mustParse(t, fixture)
	out, _ := roundTrip(t, d)
	assertComments(t, out)
}

// SetBool is what a caller needs for a boolean key: SetScalar forces string semantics, so
// `use_tmpfs: "true"` would come back as a string and fail to decode into a bool.
func TestSetBoolWritesAPlainBoolean(t *testing.T) {
	d, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetBool([]string{"test", "use_tmpfs"}, true); err != nil {
		t.Fatal(err)
	}
	out, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "use_tmpfs: true\n") {
		t.Errorf("want a plain `use_tmpfs: true`, got:\n%s", out)
	}
	for _, c := range fixtureComments {
		if !strings.Contains(string(out), c) {
			t.Errorf("comment %q lost:\n%s", c, out)
		}
	}

	var decoded struct {
		Test struct {
			UseTmpfs bool `yaml:"use_tmpfs"`
		} `yaml:"test"`
	}
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output does not decode into a bool: %v", err)
	}
	if !decoded.Test.UseTmpfs {
		t.Error("use_tmpfs decoded as false")
	}
}

func TestSetBoolReplacesAnExistingValue(t *testing.T) {
	d, err := Parse([]byte("test:\n  use_tmpfs: \"no\" # keep\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetBool([]string{"test", "use_tmpfs"}, false); err != nil {
		t.Fatal(err)
	}
	out, _ := d.Bytes()
	if !strings.Contains(string(out), "use_tmpfs: false # keep") {
		t.Errorf("want `use_tmpfs: false # keep`, got:\n%s", out)
	}
}
