package tray

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildActionNotificationSuccess(t *testing.T) {
	summary, body := BuildActionNotification("mybundle", "up", nil)
	if !strings.Contains(summary, "mybundle") {
		t.Errorf("summary = %q, want it to mention mybundle", summary)
	}
	if strings.Contains(strings.ToLower(body+summary), "fail") {
		t.Errorf("success notification mentions failure: summary=%q body=%q", summary, body)
	}
}

func TestBuildActionNotificationFailure(t *testing.T) {
	summary, body := BuildActionNotification("mybundle", "up", errors.New("port already in use"))
	if !strings.Contains(strings.ToLower(summary), "fail") {
		t.Errorf("summary = %q, want it to mention failure", summary)
	}
	if !strings.Contains(body, "port already in use") {
		t.Errorf("body = %q, want it to include the underlying error", body)
	}
}

func TestBuildTransitionNotificationMentionsProjectAndError(t *testing.T) {
	summary, body := BuildTransitionNotification("mybundle")
	if !strings.Contains(summary, "mybundle") {
		t.Errorf("summary = %q, want it to mention mybundle", summary)
	}
	if body == "" {
		t.Error("body is empty, want an explanation")
	}
}

func TestNotifyCallsSendNotification(t *testing.T) {
	orig := sendNotification
	defer func() { sendNotification = orig }()

	var gotSummary, gotBody string
	sendNotification = func(summary, body string) error {
		gotSummary, gotBody = summary, body
		return nil
	}

	if err := Notify("hello", "world"); err != nil {
		t.Fatalf("Notify() error = %v", err)
	}
	if gotSummary != "hello" || gotBody != "world" {
		t.Errorf("sendNotification called with (%q, %q), want (hello, world)", gotSummary, gotBody)
	}
}

func TestNotifyPropagatesError(t *testing.T) {
	orig := sendNotification
	defer func() { sendNotification = orig }()
	sendNotification = func(summary, body string) error { return errors.New("no notification server") }

	if err := Notify("hello", "world"); err == nil {
		t.Fatal("Notify() error = nil, want error when sendNotification fails")
	}
}
