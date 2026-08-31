package report

import (
	"bytes"
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
// defines no verbosity option. The `json` format raises no warning, Rector puts its own console
// output in quiet mode for it, and it is the format that outlives `gitlab`, which Rector says it
// removes in the next minor.
//
// That still does not make the file the document alone — anything else printing on stdout inside
// the container lands in it — so the document is located rather than assumed; see
// decodeRectorDocument.
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
	document, err := decodeRectorDocument(data)
	if err != nil {
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

// decodeRectorDocument reads Rector's document out of the file its stdout was captured into, which
// is not always the document alone.
//
// Rector writes the JSON with `echo`, below the console it otherwise silences, so anything else
// that reaches the same stream — a Symfony warning block, a PHP `Warning:` raised by an autoloaded
// file, a bootstrap notice from the application — lands in the file next to it. That is the whole
// history of this report file: first "invalid character 'W' looking for beginning of value" from
// the `gitlab` format's deprecation block, then the same 'W' again with `json`, from something else
// printing on the way. Which line it is differs per environment, and the QA step cannot control
// every library the analysed application loads.
//
// So the document is located rather than assumed: the plain decode is tried first, and a file that
// is not one JSON value is searched backwards for the last line that opens an object, which is
// where Rector's own output begins. Trailing output is ignored the same way, because the decoder
// stops at the end of the value. A file with no document in it at all is still an error — that is a
// tool that did not report, and silently counting it as zero findings is the failure the report
// package exists to prevent.
func decodeRectorDocument(data []byte) (rectorDocument, error) {
	var document rectorDocument
	if err := json.Unmarshal(data, &document); err == nil {
		return document, nil
	}

	for _, offset := range rectorDocumentStarts(data) {
		var candidate rectorDocument
		if err := json.NewDecoder(bytes.NewReader(data[offset:])).Decode(&candidate); err == nil {
			return candidate, nil
		}
	}

	return rectorDocument{}, fmt.Errorf("it starts with %q", rectorHead(data))
}

// rectorDocumentStarts lists the offsets a JSON object could begin at, last first: Rector's
// document is the last thing it prints, and a `{` opening a line in the preamble is far more likely
// to be noise than the report.
func rectorDocumentStarts(data []byte) []int {
	var offsets []int
	// A line that opens an object at column 0 is the document: Rector pretty-prints it, so every
	// nested object is indented and only the outermost one starts a line.
	for start := 0; start < len(data); {
		if data[start] == '{' {
			offsets = append(offsets, start)
		}
		index := bytes.IndexByte(data[start:], '\n')
		if index < 0 {
			break
		}
		start += index + 1
	}

	// Reversed in place: the caller tries the last candidate first.
	for i, j := 0, len(offsets)-1; i < j; i, j = i+1, j-1 {
		offsets[i], offsets[j] = offsets[j], offsets[i]
	}
	return offsets
}

// rectorHead is the start of an unusable report, quoted in the error so the next occurrence names
// what printed instead of leaving a character code to guess from. The tool's stdout is the report
// file, so this text is the only trace of it anywhere — it never reaches the job log, which sees
// stderr alone.
func rectorHead(data []byte) string {
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) == 3 {
			break
		}
	}

	head := strings.Join(lines, " / ")
	if len(head) > 300 {
		head = head[:300] + "..."
	}
	return head
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
