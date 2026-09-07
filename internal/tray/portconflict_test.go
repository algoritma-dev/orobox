package tray

import (
	"errors"
	"strings"
	"testing"
)

func TestIsPortConflictErrorTrueCases(t *testing.T) {
	cases := []string{
		`Error response from daemon: driver failed programming external connectivity on endpoint mybundle-web-1: Bind for 0.0.0.0:8080 failed: port is already allocated`,
		`Error starting userland proxy: listen tcp4 0.0.0.0:5432: bind: address already in use`,
	}
	for _, output := range cases {
		if !IsPortConflictError(output) {
			t.Errorf("IsPortConflictError(%q) = false, want true", output)
		}
	}
}

func TestIsPortConflictErrorFalseForUnrelatedOutput(t *testing.T) {
	if IsPortConflictError("Error response from daemon: No such image: foo") {
		t.Error("IsPortConflictError() = true, want false for an unrelated error")
	}
}

func TestBuildSwitchFailureNotificationPortConflict(t *testing.T) {
	output := "Bind for 0.0.0.0:8080 failed: port is already allocated"
	summary, body := BuildSwitchFailureNotification("mybundle", errors.New("exit status 1"), output)

	if !strings.Contains(strings.ToLower(summary+body), "port") {
		t.Errorf("summary=%q body=%q, want a mention of the port conflict", summary, body)
	}
	if strings.Contains(body, "exit status 1") {
		t.Errorf("body = %q, want the raw docker error replaced by a friendly message", body)
	}
}

func TestBuildSwitchFailureNotificationGenericError(t *testing.T) {
	summary, body := BuildSwitchFailureNotification("mybundle", errors.New("some other failure"), "some other failure")

	if !strings.Contains(body, "some other failure") {
		t.Errorf("body = %q, want the underlying error included for a non-port failure", body)
	}
	_ = summary
}
