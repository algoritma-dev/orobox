package project

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseServiceStatusesArrayForm(t *testing.T) {
	input := `[{"Service":"web","State":"running","Health":"healthy","ExitCode":0},{"Service":"db","State":"running","Health":"","ExitCode":0}]`

	got := ParseServiceStatuses([]byte(input))

	want := map[string]ServiceStatus{
		"web": {Service: "web", State: "running", Health: "healthy", ExitCode: 0},
		"db":  {Service: "db", State: "running", Health: "", ExitCode: 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseServiceStatuses(array) = %+v, want %+v", got, want)
	}
}

func TestParseServiceStatusesNDJSONForm(t *testing.T) {
	input := "{\"Service\":\"web\",\"State\":\"running\",\"Health\":\"healthy\",\"ExitCode\":0}\n{\"Service\":\"db\",\"State\":\"exited\",\"Health\":\"\",\"ExitCode\":1}\n"

	got := ParseServiceStatuses([]byte(input))

	want := map[string]ServiceStatus{
		"web": {Service: "web", State: "running", Health: "healthy", ExitCode: 0},
		"db":  {Service: "db", State: "exited", Health: "", ExitCode: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseServiceStatuses(ndjson) = %+v, want %+v", got, want)
	}
}

func TestParseServiceStatusesEmptyOutput(t *testing.T) {
	got := ParseServiceStatuses([]byte(""))

	if len(got) != 0 {
		t.Errorf("ParseServiceStatuses(empty) = %+v, want empty map", got)
	}
}

func TestParseServiceStatusesMalformedOutput(t *testing.T) {
	got := ParseServiceStatuses([]byte("not json at all"))

	if len(got) != 0 {
		t.Errorf("ParseServiceStatuses(malformed) = %+v, want empty map", got)
	}
}

func TestStateFromStatuses(t *testing.T) {
	kernel := []string{"web", "php-fpm-app", "ws", "consumer", "cron"}

	tests := []struct {
		name      string
		statuses  map[string]ServiceStatus
		wantState State
	}{
		{
			name:      "no kernel service present",
			statuses:  map[string]ServiceStatus{},
			wantState: StateDown,
		},
		{
			name: "all kernel services running and healthy",
			statuses: map[string]ServiceStatus{
				"web":         {Service: "web", State: "running", Health: "healthy"},
				"php-fpm-app": {Service: "php-fpm-app", State: "running", Health: ""},
				"ws":          {Service: "ws", State: "running", Health: "healthy"},
				"consumer":    {Service: "consumer", State: "running", Health: "healthy"},
				"cron":        {Service: "cron", State: "running", Health: "healthy"},
			},
			wantState: StateUp,
		},
		{
			name: "some running some not",
			statuses: map[string]ServiceStatus{
				"web": {Service: "web", State: "running", Health: "healthy"},
				"ws":  {Service: "ws", State: "exited", Health: "", ExitCode: 0},
			},
			wantState: StatePartial,
		},
		{
			name: "a kernel service exited non-zero",
			statuses: map[string]ServiceStatus{
				"web":      {Service: "web", State: "running", Health: "healthy"},
				"consumer": {Service: "consumer", State: "exited", Health: "", ExitCode: 1},
			},
			wantState: StateError,
		},
		{
			name: "a kernel service is unhealthy",
			statuses: map[string]ServiceStatus{
				"web": {Service: "web", State: "running", Health: "unhealthy"},
			},
			wantState: StateError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StateFromStatuses(tt.statuses, kernel)
			if got != tt.wantState {
				t.Errorf("StateFromStatuses() = %v, want %v", got, tt.wantState)
			}
		})
	}
}

func TestStatusReturnsErrorStateWhenQueryFails(t *testing.T) {
	orig := runPS
	defer func() { runPS = orig }()
	runPS = func(args []string) ([]byte, error) {
		return nil, errors.New("boom")
	}

	p := Project{Name: "mybundle", InternalDir: t.TempDir()}
	if got := p.Status(); got != StateError {
		t.Errorf("Status() = %v, want StateError", got)
	}
}

const allKernelServicesUpJSON = `[{"Service":"web","State":"running","Health":"healthy"},{"Service":"php-fpm-app","State":"running","Health":"healthy"},{"Service":"ws","State":"running","Health":"healthy"},{"Service":"consumer","State":"running","Health":"healthy"},{"Service":"cron","State":"running","Health":"healthy"}]`

func TestStatusMapsParsedOutputThroughStateMachine(t *testing.T) {
	origPS, origDB := runPS, runDBQuery
	defer func() { runPS, runDBQuery = origPS, origDB }()
	runPS = func(args []string) ([]byte, error) { return []byte(allKernelServicesUpJSON), nil }
	runDBQuery = func(args []string) ([]byte, error) { return []byte("1"), nil }

	p := Project{Name: "mybundle", InternalDir: t.TempDir()}
	if got := p.Status(); got != StateUp {
		t.Errorf("Status() = %v, want StateUp", got)
	}
}

func TestStatusReportsUpNotInstalledWhenDatabaseIsNot(t *testing.T) {
	origPS, origDB := runPS, runDBQuery
	defer func() { runPS, runDBQuery = origPS, origDB }()
	runPS = func(args []string) ([]byte, error) { return []byte(allKernelServicesUpJSON), nil }
	runDBQuery = func(args []string) ([]byte, error) { return []byte("0"), nil }

	p := Project{Name: "mybundle", InternalDir: t.TempDir()}
	if got := p.Status(); got != StateUpNotInstalled {
		t.Errorf("Status() = %v, want StateUpNotInstalled", got)
	}
}

func TestStatusReportsErrorWhenInstallCheckFails(t *testing.T) {
	origPS, origDB := runPS, runDBQuery
	defer func() { runPS, runDBQuery = origPS, origDB }()
	runPS = func(args []string) ([]byte, error) { return []byte(allKernelServicesUpJSON), nil }
	runDBQuery = func(args []string) ([]byte, error) { return []byte("connection refused"), errors.New("exit status 1") }

	p := Project{Name: "mybundle", InternalDir: t.TempDir()}
	if got := p.Status(); got != StateError {
		t.Errorf("Status() = %v, want StateError when the install check itself fails", got)
	}
}

func TestStatusDoesNotCheckInstallWhenNotUp(t *testing.T) {
	origPS, origDB := runPS, runDBQuery
	defer func() { runPS, runDBQuery = origPS, origDB }()
	runPS = func(args []string) ([]byte, error) { return []byte("[]"), nil }
	dbQueried := false
	runDBQuery = func(args []string) ([]byte, error) {
		dbQueried = true
		return []byte("1"), nil
	}

	p := Project{Name: "mybundle", InternalDir: t.TempDir()}
	if got := p.Status(); got != StateDown {
		t.Errorf("Status() = %v, want StateDown", got)
	}
	if dbQueried {
		t.Error("Status() queried install state for a project that is not up, want it skipped")
	}
}
