package report

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// TestFailure is one failing test, flattened to what a single output line needs.
type TestFailure struct {
	Suite   string
	Test    string
	Kind    string // "failure" for an assertion, "error" for an exception
	Message string
	File    string
	Line    int
}

// TestCounts is the run's tally, which is what a passing run prints instead of nothing: a test run
// that reports no failures because it ran no tests is a different outcome from one that passed.
type TestCounts struct {
	Passed  int
	Failed  int
	Errored int
}

// parsedSuite is a typed view of a suite, used only here.
//
// MergeJUnit models the same elements as opaque innerxml on purpose, so that nothing PHPUnit or an
// extension emits is dropped from the document GitLab reads. That is the right trade there and the
// wrong one here, where the point is to read inside a test case.
type parsedSuite struct {
	Name   string        `xml:"name,attr"`
	Suites []parsedSuite `xml:"testsuite"`
	Cases  []parsedCase  `xml:"testcase"`
}

type parsedCase struct {
	Name     string         `xml:"name,attr"`
	Class    string         `xml:"class,attr"`
	File     string         `xml:"file,attr"`
	Line     int            `xml:"line,attr"`
	Failures []parsedResult `xml:"failure"`
	Errors   []parsedResult `xml:"error"`
}

type parsedResult struct {
	Body string `xml:",chardata"`
}

type parsedRoot struct {
	XMLName xml.Name      `xml:"testsuites"`
	Suites  []parsedSuite `xml:"testsuite"`
}

// FailuresFromJUnit reads the failing tests and the run's tally out of a JUnit document.
//
// An empty document is not an error: it is the shape of a run cancelled before PHPUnit wrote
// anything, the same case MergeJUnit tolerates.
func FailuresFromJUnit(doc []byte) ([]TestFailure, TestCounts, error) {
	trimmed := bytes.TrimSpace(doc)
	if len(trimmed) == 0 {
		return nil, TestCounts{}, nil
	}

	var root parsedRoot
	if err := xml.Unmarshal(trimmed, &root); err != nil {
		// A bare <testsuite> root is legal for a single-suite run, so it is retried before the
		// document is called invalid — the same fallback MergeJUnit makes.
		var single parsedSuite
		if singleErr := xml.Unmarshal(trimmed, &single); singleErr != nil {
			return nil, TestCounts{}, fmt.Errorf("the JUnit document is not valid XML: %w", err)
		}
		root.Suites = []parsedSuite{single}
	}

	var failures []TestFailure
	var counts TestCounts
	for _, suite := range root.Suites {
		collectFailures(suite, &failures, &counts)
	}
	return failures, counts, nil
}

// collectFailures walks a suite and its nested suites. PHPUnit nests one suite element per test
// class inside the configured suite, so a walk that stopped at the top level would find no cases.
func collectFailures(suite parsedSuite, failures *[]TestFailure, counts *TestCounts) {
	for _, nested := range suite.Suites {
		collectFailures(nested, failures, counts)
	}

	for _, c := range suite.Cases {
		switch {
		case len(c.Failures) > 0:
			counts.Failed++
			*failures = append(*failures, testFailure(c, "failure", c.Failures[0].Body))
		case len(c.Errors) > 0:
			counts.Errored++
			*failures = append(*failures, testFailure(c, "error", c.Errors[0].Body))
		default:
			counts.Passed++
		}
	}
}

// testFailure builds one entry, taking the file and line from the case's own attributes.
//
// PHPUnit repeats the location at the end of the failure body as prose, but the attributes are
// what it writes for every case, so they are the ones read here.
func testFailure(c parsedCase, kind, body string) TestFailure {
	return TestFailure{
		Suite:   c.Class,
		Test:    c.Name,
		Kind:    kind,
		Message: failureMessage(c, body),
		File:    c.File,
		Line:    c.Line,
	}
}

// failureMessage strips the two things PHPUnit wraps around the actual message: a first line
// repeating the test's own name, and a trailing file:line that the File and Line fields already
// carry.
func failureMessage(c parsedCase, body string) string {
	lines := strings.Split(strings.TrimSpace(body), "\n")

	header := c.Class + "::" + c.Name
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == header {
		lines = lines[1:]
	}
	for len(lines) > 0 {
		last := strings.TrimSpace(lines[len(lines)-1])
		if last == "" || (c.File != "" && strings.HasPrefix(last, c.File+":")) {
			lines = lines[:len(lines)-1]
			continue
		}
		break
	}

	return collapseWhitespace(strings.Join(lines, " "))
}
