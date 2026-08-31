package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// rectorTool is the report name Rector's own document arrives under, which is the file name the QA
// step writes it to.
const rectorTool = "rector"

// Rector is the one tool whose report is converted rather than forwarded.
//
// Its `gitlab` output format exists but cannot be used: Rector prints a deprecation warning for it
// on stdout, which is the stream the report file captures, so the document arrives with a Symfony
// warning block in front of it — the "invalid character 'W' looking for beginning of value" that
// made the whole merged report fail. The warning cannot be silenced either, because `process`
// defines no verbosity option. The `json` format raises no warning, and Rector puts its console
// output in quiet mode for that format on purpose, so the file holds the document and nothing else.
// It is also the format that outlives `gitlab`, which Rector says it removes in the next minor.
//
// rectorDocument is that format: a summary object rather than a list of findings.
type rectorDocument struct {
	FileDiffs []struct {
		File           string   `json:"file"`
		Diff           string   `json:"diff"`
		AppliedRectors []string `json:"applied_rectors"`
	} `json:"file_diffs"`
	Errors []struct {
		Message  string `json:"message"`
		File     string `json:"file"`
		CausedBy string `json:"caused_by"`
		Line     int    `json:"line"`
	} `json:"errors"`
}

// rectorHunkHeader matches the first line number a unified diff hunk names, which is the line the
// finding is reported at. Rector's own GitLab formatter derived it the same way, from the same
// header, so a finding lands where it always did.
var rectorHunkHeader = regexp.MustCompile(`@@[^0-9@]*([0-9]+)`)

// rectorIssues turns Rector's JSON document into the CodeClimate issues every other tool emits
// itself.
//
// The two halves of the document mean different things and are graded differently. A file diff is
// a rule that wants to rewrite the file — a style finding, and what a clean run has none of. A
// system error is Rector failing on a file: a parse error, a rule that threw. GitLab shows the
// first as minor and the second as blocking, which is the same split Rector's GitLab formatter
// made.
func rectorIssues(data []byte) ([]map[string]any, error) {
	var document rectorDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}

	issues := []map[string]any{}

	for _, diff := range document.FileDiffs {
		rules := strings.Join(rectorShortClasses(diff.AppliedRectors), " / ")
		issues = append(issues, map[string]any{
			"fingerprint": rectorFingerprint(diff.File + ";" + diff.Diff),
			"type":        "issue",
			"categories":  []any{"Style"},
			"severity":    "minor",
			"description": rules,
			"check_name":  rules,
			"content":     map[string]any{"body": diff.Diff},
			"location": map[string]any{
				"path":  diff.File,
				"lines": map[string]any{"begin": rectorFirstLine(diff.Diff)},
			},
		})
	}

	for _, systemError := range document.Errors {
		issues = append(issues, map[string]any{
			"fingerprint": rectorFingerprint(fmt.Sprintf("%s;%d;%s", systemError.File, systemError.Line, systemError.Message)),
			"type":        "issue",
			"categories":  []any{"Bug Risk"},
			"severity":    "blocker",
			"description": systemError.Message,
			"check_name":  systemError.CausedBy,
			"location": map[string]any{
				"path":  systemError.File,
				"lines": map[string]any{"begin": systemError.Line},
			},
		})
	}

	return issues, nil
}

// rectorShortClasses drops the namespace from each applied rule, so a finding reads
// "AddVoidReturnTypeWhereNoReturnRector" rather than its fully qualified name. Rector's GitLab
// formatter reported the short names, and GitLab shows this string as the finding's title.
func rectorShortClasses(classes []string) []string {
	short := make([]string, 0, len(classes))
	for _, class := range classes {
		// Rector's classes are PHP names, so the separator is a backslash whichever platform this
		// runs on.
		if index := strings.LastIndex(class, `\`); index >= 0 {
			class = class[index+1:]
		}
		short = append(short, class)
	}
	return short
}

// rectorFirstLine reads the line a diff starts at out of its first hunk header. A diff Rector wrote
// always has one; a document that somehow has none reports the finding at line 0, which is what
// Rector itself falls back to.
func rectorFirstLine(diff string) int {
	match := rectorHunkHeader.FindStringSubmatch(diff)
	if match == nil {
		return 0
	}
	line, err := strconv.Atoi(match[1])
	if err != nil {
		return 0
	}
	return line
}

// rectorFingerprint is what GitLab deduplicates findings by across pipelines, so it has to be
// stable for an unchanged finding and different for a changed one — nothing more. The hash is
// Orobox's own rather than Rector's, because the json format does not carry the one Rector's GitLab
// formatter computed.
func rectorFingerprint(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}
