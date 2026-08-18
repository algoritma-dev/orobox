package report

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

// junitSuites is a JUnit document's root. PHPUnit always writes <testsuites>, but a document
// produced by a single run can legitimately have <testsuite> as its root, so both are accepted.
type junitSuites struct {
	XMLName xml.Name     `xml:"testsuites"`
	Suites  []junitSuite `xml:"testsuite"`
}

// junitSuite carries its attributes and its children verbatim. Modelling them would mean tracking
// every element PHPUnit and its extensions can emit — failures, errors, skipped, system-out,
// properties — and silently dropping whatever the struct does not name. innerxml round-trips the
// lot without an opinion.
type junitSuite struct {
	XMLName xml.Name   `xml:"testsuite"`
	Attrs   []xml.Attr `xml:",any,attr"`
	Inner   []byte     `xml:",innerxml"`
}

// MergeJUnit combines JUnit documents into one <testsuites> root.
//
// The deploy pipeline runs one PHPUnit invocation per configured suite, so a stage running unit
// and functional tests produces two documents; `orobox test`, which runs PHPUnit once, is the
// degenerate case of the same merge. GitLab accepts a glob of several files, but one file keeps
// --report-path meaning a file rather than sometimes a directory.
//
// Empty input is not an error: it is the shape of a run that was cancelled before PHPUnit wrote
// anything, and a valid empty document is what keeps the artifact present.
func MergeJUnit(docs [][]byte) ([]byte, error) {
	merged := junitSuites{}

	for i, doc := range docs {
		trimmed := bytes.TrimSpace(doc)
		if len(trimmed) == 0 {
			continue
		}

		var root junitSuites
		if err := xml.Unmarshal(trimmed, &root); err != nil {
			// A bare <testsuite> root: unmarshalling into junitSuites fails on the element name,
			// so it is retried as a single suite before the document is called invalid.
			var single junitSuite
			if singleErr := xml.Unmarshal(trimmed, &single); singleErr != nil {
				return nil, fmt.Errorf("JUnit document %d is not valid XML: %w", i+1, err)
			}
			merged.Suites = append(merged.Suites, single)
			continue
		}
		merged.Suites = append(merged.Suites, root.Suites...)
	}

	body, err := xml.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("could not encode the merged JUnit document: %w", err)
	}
	return append([]byte(xml.Header), body...), nil
}
