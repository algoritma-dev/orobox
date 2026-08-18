package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathPrefixRewrite(t *testing.T) {
	tests := []struct {
		name   string
		prefix PathPrefix
		in     string
		want   string
	}{
		{
			name:   "absolute container path becomes repository relative",
			prefix: PathPrefix{ContainerRoot: "/var/www/oro"},
			in:     "/var/www/oro/src/Acme/Bundle/File.php",
			want:   "src/Acme/Bundle/File.php",
		},
		{
			name:   "already relative path is left alone",
			prefix: PathPrefix{ContainerRoot: "/var/www/oro"},
			in:     "src/Acme/Bundle/File.php",
			want:   "src/Acme/Bundle/File.php",
		},
		{
			name:   "leading ./ is stripped",
			prefix: PathPrefix{ContainerRoot: "/var/www/oro"},
			in:     "./src/Acme/Bundle/File.php",
			want:   "src/Acme/Bundle/File.php",
		},
		{
			name:   "monorepo subdirectory is prefixed back",
			prefix: PathPrefix{ContainerRoot: "/var/www/oro", RepoSubdir: "apps/shop"},
			in:     "/var/www/oro/src/Acme/Bundle/File.php",
			want:   "apps/shop/src/Acme/Bundle/File.php",
		},
		{
			name:   "a path outside the application root is left alone",
			prefix: PathPrefix{ContainerRoot: "/var/www/oro"},
			in:     "/tmp/generated.php",
			want:   "/tmp/generated.php",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.prefix.Rewrite(tc.in); got != tc.want {
				t.Errorf("Rewrite(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMergeCodeQualityConcatenatesAndCounts(t *testing.T) {
	first := []byte(`[{"description":"a","fingerprint":"f1","location":{"path":"/var/www/oro/src/A.php","lines":{"begin":3}}}]`)
	second := []byte(`[{"description":"b","fingerprint":"f2","location":{"path":"src/B.php","lines":{"begin":9}}},
	                   {"description":"c","fingerprint":"f3","location":{"path":"src/C.php","lines":{"begin":1}}}]`)

	result, err := MergeCodeQuality([]ToolReport{
		{Tool: "phpstan", Data: first},
		{Tool: "rector", Data: second},
	}, PathPrefix{ContainerRoot: "/var/www/oro"})
	if err != nil {
		t.Fatalf("MergeCodeQuality returned %v", err)
	}

	var issues []map[string]any
	if err := json.Unmarshal(result.Data, &issues); err != nil {
		t.Fatalf("merged document is not valid JSON: %v", err)
	}
	if len(issues) != 3 {
		t.Fatalf("merged %d issues, want 3", len(issues))
	}

	location := issues[0]["location"].(map[string]any)
	if got := location["path"]; got != "src/A.php" {
		t.Errorf("first issue path = %v, want src/A.php", got)
	}
	if result.Counts["phpstan"] != 1 || result.Counts["rector"] != 2 {
		t.Errorf("Counts = %v, want phpstan:1 rector:2", result.Counts)
	}
}

func TestMergeCodeQualityKeepsUnmodelledFields(t *testing.T) {
	data := []byte(`[{"description":"a","fingerprint":"f1","severity":"major","check_name":"Rule",
	                  "categories":["Style"],"location":{"path":"src/A.php","lines":{"begin":3,"end":5}}}]`)

	result, err := MergeCodeQuality([]ToolReport{{Tool: "phpstan", Data: data}}, PathPrefix{ContainerRoot: "/var/www/oro"})
	if err != nil {
		t.Fatalf("MergeCodeQuality returned %v", err)
	}

	var issues []map[string]any
	if err := json.Unmarshal(result.Data, &issues); err != nil {
		t.Fatalf("merged document is not valid JSON: %v", err)
	}
	if _, ok := issues[0]["categories"]; !ok {
		t.Error("categories was dropped; fields Orobox does not model must survive the merge")
	}
	location := issues[0]["location"].(map[string]any)
	lines := location["lines"].(map[string]any)
	if _, ok := lines["end"]; !ok {
		t.Error("lines.end was dropped")
	}
}

func TestMergeCodeQualityEmptyAndMissingInput(t *testing.T) {
	result, err := MergeCodeQuality([]ToolReport{
		{Tool: "phpstan", Data: nil},
		{Tool: "rector", Data: []byte("  ")},
		{Tool: "eslint", Data: []byte("[]")},
	}, PathPrefix{ContainerRoot: "/var/www/oro"})
	if err != nil {
		t.Fatalf("MergeCodeQuality returned %v", err)
	}
	if string(result.Data) != "[]" {
		t.Errorf("Data = %s, want an empty JSON array", result.Data)
	}
	for _, tool := range []string{"phpstan", "rector", "eslint"} {
		if result.Counts[tool] != 0 {
			t.Errorf("Counts[%s] = %d, want 0", tool, result.Counts[tool])
		}
	}
}

func TestMergeCodeQualityNamesTheToolInAnError(t *testing.T) {
	_, err := MergeCodeQuality([]ToolReport{
		{Tool: "phpstan", Data: []byte("[]")},
		{Tool: "rector", Data: []byte("PHP Fatal error: out of memory")},
	}, PathPrefix{ContainerRoot: "/var/www/oro"})
	if err == nil {
		t.Fatal("invalid JSON must be an error")
	}
	if !strings.Contains(err.Error(), "rector") {
		t.Errorf("error %q does not name the tool that produced the bad document", err)
	}
}

func TestMergeCodeQualityAgainstRealToolOutput(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}

	var reports []ToolReport
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join("testdata", entry.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", entry.Name(), err)
		}
		reports = append(reports, ToolReport{
			Tool: strings.TrimSuffix(entry.Name(), ".json"),
			Data: data,
		})
	}
	if len(reports) == 0 {
		t.Fatal("no captured tool output in testdata: the fixtures are what keep the normalisation honest")
	}

	result, err := MergeCodeQuality(reports, PathPrefix{ContainerRoot: "/var/www/oro"})
	if err != nil {
		t.Fatalf("MergeCodeQuality returned %v", err)
	}

	var issues []map[string]any
	if err := json.Unmarshal(result.Data, &issues); err != nil {
		t.Fatalf("merged document is not valid JSON: %v", err)
	}
	for i, issue := range issues {
		location, ok := issue["location"].(map[string]any)
		if !ok {
			t.Fatalf("issue %d has no location object", i)
		}
		filePath, _ := location["path"].(string)
		if filePath == "" {
			t.Errorf("issue %d has an empty path", i)
		}
		if strings.HasPrefix(filePath, "/") {
			t.Errorf("issue %d path %q is still absolute after the merge", i, filePath)
		}
	}
}
