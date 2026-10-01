package composeoverride

import (
	"strings"
	"testing"

	yamlv3 "gopkg.in/yaml.v3"
)

const (
	testBase = "/proj"
	testHome = "/home/u"
)

// resolveDoc resolves src and decodes the result into a generic map so assertions do not depend
// on the encoder's formatting.
func resolveDoc(t *testing.T, src string) map[string]any {
	t.Helper()
	out, err := Resolve([]byte(src), testBase, testHome)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var doc map[string]any
	if err := yamlv3.Unmarshal(out, &doc); err != nil {
		t.Fatalf("resolved output is not valid YAML: %v\n%s", err, out)
	}
	return doc
}

func service(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	svc, ok := doc["services"].(map[string]any)[name].(map[string]any)
	if !ok {
		t.Fatalf("service %q missing in %v", name, doc)
	}
	return svc
}

func strList(t *testing.T, v any) []string {
	t.Helper()
	items, ok := v.([]any)
	if !ok {
		t.Fatalf("expected a list, got %T (%v)", v, v)
	}
	out := make([]string, len(items))
	for i, it := range items {
		s, ok := it.(string)
		if !ok {
			t.Fatalf("expected string item, got %T", it)
		}
		out[i] = s
	}
	return out
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveShortVolumes(t *testing.T) {
	doc := resolveDoc(t, `
services:
  web:
    volumes:
      - ./a:/b:ro
      - ~/x:/y
      - ../up:/up
      - .:/root
      - named:/data
      - /abs:/y
      - ${VAR}/x:/y
      - ./dir/:/trail
      - /only/destination
`)
	assertStrings(t, strList(t, service(t, doc, "web")["volumes"]), []string{
		"/proj/a:/b:ro",
		"/home/u/x:/y",
		"/up:/up",
		"/proj:/root",
		"named:/data",
		"/abs:/y",
		"${VAR}/x:/y",
		"/proj/dir:/trail",
		"/only/destination",
	})
}

func TestResolveLongVolumes(t *testing.T) {
	doc := resolveDoc(t, `
services:
  web:
    volumes:
      - {type: bind, source: ./a, target: /a}
      - {type: bind, source: ~/h, target: /h}
      - {type: volume, source: v, target: /v}
      - {type: volume, source: ./not-a-path, target: /w}
      - {type: tmpfs, target: /t}
`)
	vols := service(t, doc, "web")["volumes"].([]any)
	wantSource := []string{"/proj/a", "/home/u/h", "v", "./not-a-path", ""}
	for i, want := range wantSource {
		got, _ := vols[i].(map[string]any)["source"].(string)
		if got != want {
			t.Errorf("volume %d source = %q, want %q", i, got, want)
		}
	}
}

func TestResolveBuild(t *testing.T) {
	doc := resolveDoc(t, `
services:
  a:
    build: ./svc
  b:
    build:
      context: .
      dockerfile: Dockerfile.dev
  c:
    build: svc
  d:
    build: https://github.com/example/repo.git
  e:
    build:
      context: ${CTX}
`)
	if got := service(t, doc, "a")["build"]; got != "/proj/svc" {
		t.Errorf("string build = %v", got)
	}
	b := service(t, doc, "b")["build"].(map[string]any)
	if b["context"] != "/proj" || b["dockerfile"] != "Dockerfile.dev" {
		t.Errorf("mapping build = %v", b)
	}
	if got := service(t, doc, "c")["build"]; got != "/proj/svc" {
		t.Errorf("bare build = %v", got)
	}
	if got := service(t, doc, "d")["build"]; got != "https://github.com/example/repo.git" {
		t.Errorf("remote build context rewritten: %v", got)
	}
	if got := service(t, doc, "e")["build"].(map[string]any)["context"]; got != "${CTX}" {
		t.Errorf("interpolated context rewritten: %v", got)
	}
}

func TestResolveEnvFile(t *testing.T) {
	doc := resolveDoc(t, `
services:
  s:
    env_file: ./e.env
  l:
    env_file: [./a.env, foo.env, /abs.env, $X/y.env]
  m:
    env_file:
      - path: ./e
        required: false
      - ./plain
`)
	if got := service(t, doc, "s")["env_file"]; got != "/proj/e.env" {
		t.Errorf("string env_file = %v", got)
	}
	assertStrings(t, strList(t, service(t, doc, "l")["env_file"]),
		[]string{"/proj/a.env", "/proj/foo.env", "/abs.env", "$X/y.env"})
	m := service(t, doc, "m")["env_file"].([]any)
	first := m[0].(map[string]any)
	if first["path"] != "/proj/e" || first["required"] != false {
		t.Errorf("env_file path form = %v", first)
	}
	if m[1] != "/proj/plain" {
		t.Errorf("env_file mixed string item = %v", m[1])
	}
}

func TestResolveExtendsAndLabelFile(t *testing.T) {
	doc := resolveDoc(t, `
services:
  a:
    extends:
      file: ./base.yaml
      service: web
  b:
    extends: web
  c:
    label_file: ./labels
  d:
    label_file: [./l1, ./l2]
`)
	if got := service(t, doc, "a")["extends"].(map[string]any)["file"]; got != "/proj/base.yaml" {
		t.Errorf("extends.file = %v", got)
	}
	if got := service(t, doc, "a")["extends"].(map[string]any)["service"]; got != "web" {
		t.Errorf("extends.service must be untouched, got %v", got)
	}
	if got := service(t, doc, "b")["extends"]; got != "web" {
		t.Errorf("string extends must be untouched, got %v", got)
	}
	if got := service(t, doc, "c")["label_file"]; got != "/proj/labels" {
		t.Errorf("label_file string = %v", got)
	}
	assertStrings(t, strList(t, service(t, doc, "d")["label_file"]), []string{"/proj/l1", "/proj/l2"})
}

func TestResolveConfigsAndSecrets(t *testing.T) {
	doc := resolveDoc(t, `
configs:
  c:
    file: ./c.conf
  ext:
    external: true
secrets:
  s:
    file: ~/s.key
`)
	if got := doc["configs"].(map[string]any)["c"].(map[string]any)["file"]; got != "/proj/c.conf" {
		t.Errorf("configs file = %v", got)
	}
	if got := doc["secrets"].(map[string]any)["s"].(map[string]any)["file"]; got != "/home/u/s.key" {
		t.Errorf("secrets file = %v", got)
	}
}

func TestResolveInclude(t *testing.T) {
	doc := resolveDoc(t, `
include:
  - ./a.yaml
  - path: ./b.yaml
    env_file: ./e
    project_directory: ./d
  - path: [./c1.yaml, ./c2.yaml]
    env_file: [./e1, ./e2]
`)
	inc := doc["include"].([]any)
	if inc[0] != "/proj/a.yaml" {
		t.Errorf("include string = %v", inc[0])
	}
	long := inc[1].(map[string]any)
	if long["path"] != "/proj/b.yaml" || long["env_file"] != "/proj/e" || long["project_directory"] != "/proj/d" {
		t.Errorf("include long form = %v", long)
	}
	lists := inc[2].(map[string]any)
	assertStrings(t, strList(t, lists["path"]), []string{"/proj/c1.yaml", "/proj/c2.yaml"})
	assertStrings(t, strList(t, lists["env_file"]), []string{"/proj/e1", "/proj/e2"})
}

func TestResolveLeavesUnknownKeysAlone(t *testing.T) {
	doc := resolveDoc(t, `
x-custom:
  path: ./keep
services:
  web:
    image: nginx
    working_dir: ./keep
    command: ["./run.sh"]
    healthcheck:
      test: ["CMD", "./check"]
`)
	if doc["x-custom"].(map[string]any)["path"] != "./keep" {
		t.Errorf("extension field rewritten: %v", doc["x-custom"])
	}
	web := service(t, doc, "web")
	if web["working_dir"] != "./keep" {
		t.Errorf("working_dir rewritten: %v", web["working_dir"])
	}
	assertStrings(t, strList(t, web["command"]), []string{"./run.sh"})
}

func TestResolveEmptyHome(t *testing.T) {
	out, err := Resolve([]byte("services:\n  w:\n    volumes:\n      - ~/x:/y\n"), testBase, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.Contains(string(out), "~/x:/y") {
		t.Errorf("tilde must be left alone when home is unknown:\n%s", out)
	}
}

func TestResolvePreservesTags(t *testing.T) {
	out, err := Resolve([]byte(`services:
  web:
    ports: !override ["1:1"]
    labels: !reset []
    volumes: !override
      - ./a:/b
`), testBase, testHome)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	s := string(out)
	for _, want := range []string{"!override", "!reset", "/proj/a:/b"} {
		if !strings.Contains(s, want) {
			t.Errorf("output lost %q:\n%s", want, s)
		}
	}
	if strings.Count(s, "!override") != 2 {
		t.Errorf("expected two !override tags:\n%s", s)
	}
}

func TestResolvePreservesComments(t *testing.T) {
	out, err := Resolve([]byte("# head\nservices:\n  web:\n    # note\n    build: ./svc # trailing\n"), testBase, testHome)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, want := range []string{"# head", "# note", "# trailing", "/proj/svc"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output lost %q:\n%s", want, out)
		}
	}
}

func TestResolveUsesTwoSpaceIndent(t *testing.T) {
	out, err := Resolve([]byte("services:\n    web:\n        build: ./svc\n"), testBase, testHome)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.Contains(string(out), "\n  web:\n    build:") {
		t.Errorf("expected 2-space indentation:\n%s", out)
	}
}

func TestResolveEmptyDocument(t *testing.T) {
	for _, src := range []string{"", "# only a comment\n", "   \n\n"} {
		out, err := Resolve([]byte(src), testBase, testHome)
		if err != nil || out != nil {
			t.Errorf("Resolve(%q) = %q, %v; want nil, nil", src, out, err)
		}
	}
}

func TestResolveInvalidYAML(t *testing.T) {
	if _, err := Resolve([]byte("services: [unclosed"), testBase, testHome); err == nil {
		t.Fatal("expected an error for invalid YAML")
	}
}

func TestResolveIsIdempotent(t *testing.T) {
	first, err := Resolve([]byte("services:\n  w:\n    volumes:\n      - ./a:/b\n    build: ./svc\n"), testBase, testHome)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(first, testBase, testHome)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("resolving twice changed the output:\n%s\n---\n%s", first, second)
	}
}

// mustParseMapping parses src and returns its top-level mapping node.
func mustParseMapping(t *testing.T, src string) *yamlv3.Node {
	t.Helper()
	var n yamlv3.Node
	if err := yamlv3.Unmarshal([]byte(src), &n); err != nil {
		t.Fatal(err)
	}
	return n.Content[0]
}

// Compose defaults a build context to ".", which it resolves against --project-directory: the
// internal directory, not the project. A build mapping that names only a Dockerfile would build
// the wrong directory.
func TestResolveBuildMappingWithoutContext(t *testing.T) {
	doc := resolveDoc(t, `
x-build: &b
  context: ./from-anchor
services:
  a:
    build:
      dockerfile: Dockerfile.dev
  merged:
    build:
      <<: *b
      dockerfile: D
  reset:
    build: !reset null
`)
	a := service(t, doc, "a")["build"].(map[string]any)
	if a["context"] != "/proj" || a["dockerfile"] != "Dockerfile.dev" {
		t.Errorf("build without context = %v, want context /proj and the dockerfile untouched", a)
	}
	merged := service(t, doc, "merged")["build"].(map[string]any)
	if merged["context"] != "/proj/from-anchor" {
		t.Errorf("a context from a merge key must be rewritten, not replaced: %v", merged)
	}
	if got := service(t, doc, "reset")["build"]; got != nil {
		t.Errorf("a !reset build must stay unset, got %v", got)
	}
}

func TestResolveBuildAdditionalContextsSSHAndWatch(t *testing.T) {
	doc := resolveDoc(t, `
services:
  m:
    build:
      context: .
      additional_contexts:
        local: ./lib
        bare: lib
        image: docker-image://alpine:3
        svc: service:base
        oci: oci-layout://./layout
        tgt: target:base
        remote: https://github.com/x/y.git
      ssh:
        - default
        - key=~/.ssh/id
        - rel=./keys/id
    develop:
      watch:
        - path: ./src
          target: /app/src
          action: sync
  l:
    build:
      context: .
      additional_contexts:
        - local=./lib
        - image=docker-image://alpine:3
`)
	m := service(t, doc, "m")
	build := m["build"].(map[string]any)
	ac := build["additional_contexts"].(map[string]any)
	for key, want := range map[string]string{
		"local":  "/proj/lib",
		"bare":   "/proj/lib",
		"image":  "docker-image://alpine:3",
		"svc":    "service:base",
		"oci":    "oci-layout://./layout",
		"tgt":    "target:base",
		"remote": "https://github.com/x/y.git",
	} {
		if ac[key] != want {
			t.Errorf("additional_contexts.%s = %v, want %q", key, ac[key], want)
		}
	}
	assertStrings(t, strList(t, build["ssh"]), []string{"default", "key=/home/u/.ssh/id", "rel=/proj/keys/id"})
	watch := m["develop"].(map[string]any)["watch"].([]any)[0].(map[string]any)
	if watch["path"] != "/proj/src" || watch["target"] != "/app/src" {
		t.Errorf("develop.watch = %v, want path rewritten and target untouched", watch)
	}
	list := service(t, doc, "l")["build"].(map[string]any)["additional_contexts"]
	assertStrings(t, strList(t, list), []string{"local=/proj/lib", "image=docker-image://alpine:3"})
}

func TestResolveMultiDocument(t *testing.T) {
	out, err := Resolve([]byte("services:\n  a:\n    build: ./a\n---\n# nothing here\n---\nservices:\n  b:\n    volumes:\n      - ./b:/b\n"), testBase, testHome)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	docs := strings.Split(string(out), "\n---\n")
	if len(docs) != 2 {
		t.Fatalf("expected the two non-empty documents separated by ---, got:\n%s", out)
	}
	if !strings.Contains(docs[0], "/proj/a") || !strings.Contains(docs[1], "/proj/b:/b") {
		t.Errorf("every document must be resolved:\n%s", out)
	}

	out, err = Resolve([]byte("---\n# a\n---\n# b\n"), testBase, testHome)
	if err != nil || out != nil {
		t.Errorf("a file with only empty documents = %q, %v; want nil, nil", out, err)
	}
	if _, err := Resolve([]byte("services: {}\n---\n- a list\n"), testBase, testHome); err == nil {
		t.Error("a later document that is not a mapping must be an error")
	}
}

func TestResolveLeavesRemoteBuildContextsAlone(t *testing.T) {
	for _, ctx := range []string{
		"github.com/example/repo",
		"github.com/example/repo#main:dir",
		"git@github.com:example/repo.git",
		"git://example.com/repo.git",
		"https://example.com/repo.git#v1",
	} {
		doc := resolveDoc(t, "services:\n  a:\n    build: \""+ctx+"\"\n  b:\n    build:\n      context: \""+ctx+"\"\n")
		if got := service(t, doc, "a")["build"]; got != ctx {
			t.Errorf("build %q rewritten to %v", ctx, got)
		}
		if got := service(t, doc, "b")["build"].(map[string]any)["context"]; got != ctx {
			t.Errorf("build.context %q rewritten to %v", ctx, got)
		}
	}
}

// Compose interpolates "$" in every value, so a project directory that contains one must reach
// it escaped; the user's own part of the path is left as written.
func TestResolveEscapesDollarInBaseDir(t *testing.T) {
	out, err := Resolve([]byte("services:\n  w:\n    volumes:\n      - ./a:/b\n      - ./${SUB}/x:/c\n    env_file: ./e.env\n"), "/pro$j", "/ho$me")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var doc map[string]any
	if err := yamlv3.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	w := service(t, doc, "w")
	assertStrings(t, strList(t, w["volumes"]), []string{"/pro$$j/a:/b", "/pro$$j/${SUB}/x:/c"})
	if w["env_file"] != "/pro$$j/e.env" {
		t.Errorf("env_file = %v", w["env_file"])
	}
	again, err := Resolve(out, "/pro$j", "/ho$me")
	if err != nil || string(again) != string(out) {
		t.Errorf("resolving an escaped path twice changed it:\n%s\n---\n%s (%v)", out, again, err)
	}
}

// A ":" in the absolute source would make the short syntax ambiguous, so the entry is written
// in long syntax, keeping what the mode said.
func TestResolveColonInBaseDirUsesLongSyntax(t *testing.T) {
	out, err := Resolve([]byte("services:\n  w:\n    volumes:\n      - ./a:/b:ro,z\n      - ./c:/d\n      - named:/e\n"), "/pro:j", testHome)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var doc map[string]any
	if err := yamlv3.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	vols := service(t, doc, "w")["volumes"].([]any)
	first, ok := vols[0].(map[string]any)
	if !ok {
		t.Fatalf("expected long syntax, got %v", vols[0])
	}
	if first["type"] != "bind" || first["source"] != "/pro:j/a" || first["target"] != "/b" || first["read_only"] != true {
		t.Errorf("long syntax = %v", first)
	}
	bind := first["bind"].(map[string]any)
	if bind["selinux"] != "z" || bind["create_host_path"] != true {
		t.Errorf("bind options = %v, want selinux z and create_host_path true (what the short syntax implied)", bind)
	}
	second := vols[1].(map[string]any)
	if second["source"] != "/pro:j/c" || second["target"] != "/d" || second["read_only"] != nil {
		t.Errorf("long syntax without mode = %v", second)
	}
	if vols[2] != "named:/e" {
		t.Errorf("a named volume must stay as written, got %v", vols[2])
	}

	_, err = Resolve([]byte("services:\n  w:\n    volumes:\n      - ./a:/b:nocopy\n"), "/pro:j", testHome)
	if err == nil || !strings.Contains(err.Error(), "/pro:j") {
		t.Errorf("an unconvertible mode must be an error naming the project directory, got %v", err)
	}
}

// Compose expands any leading "~", "~user" included, as the home directory plus the rest.
func TestResolveTildeUser(t *testing.T) {
	doc := resolveDoc(t, "services:\n  w:\n    volumes:\n      - ~user/x:/y\n    env_file: ~other/e.env\n")
	w := service(t, doc, "w")
	assertStrings(t, strList(t, w["volumes"]), []string{"/home/u/user/x:/y"})
	if w["env_file"] != "/home/u/other/e.env" {
		t.Errorf("env_file = %v", w["env_file"])
	}
}
