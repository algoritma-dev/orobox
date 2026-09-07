package tray

import (
	"errors"
	"testing"
)

func TestClassifyDockerInfoErrorPermissionDenied(t *testing.T) {
	output := `permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock: Get "http://%2Fvar%2Frun%2Fdocker.sock/v1.24/info": dial unix /var/run/docker.sock: connect: permission denied`

	if got := classifyDockerInfoError(output); got != DockerPermissionDenied {
		t.Errorf("classifyDockerInfoError(permission denied) = %v, want DockerPermissionDenied", got)
	}
}

func TestClassifyDockerInfoErrorDaemonNotRunning(t *testing.T) {
	output := "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?"

	if got := classifyDockerInfoError(output); got != DockerDaemonNotRunning {
		t.Errorf("classifyDockerInfoError(daemon down) = %v, want DockerDaemonNotRunning", got)
	}
}

func TestClassifyDockerInfoErrorUnknownDefaultsToDaemonNotRunning(t *testing.T) {
	output := "some unexpected docker error"

	if got := classifyDockerInfoError(output); got != DockerDaemonNotRunning {
		t.Errorf("classifyDockerInfoError(unknown) = %v, want DockerDaemonNotRunning", got)
	}
}

func TestCheckDockerReportsBinaryMissing(t *testing.T) {
	orig := lookPathDocker
	defer func() { lookPathDocker = orig }()
	lookPathDocker = func() error { return errors.New("not found") }

	if got := CheckDocker(); got != DockerBinaryMissing {
		t.Errorf("CheckDocker() = %v, want DockerBinaryMissing", got)
	}
}

func TestCheckDockerReportsOKWhenInfoSucceeds(t *testing.T) {
	origLookPath, origInfo := lookPathDocker, runDockerInfo
	defer func() { lookPathDocker, runDockerInfo = origLookPath, origInfo }()
	lookPathDocker = func() error { return nil }
	runDockerInfo = func() (string, error) { return "server version stuff", nil }

	if got := CheckDocker(); got != DockerOK {
		t.Errorf("CheckDocker() = %v, want DockerOK", got)
	}
}

func TestCheckDockerClassifiesInfoFailure(t *testing.T) {
	origLookPath, origInfo := lookPathDocker, runDockerInfo
	defer func() { lookPathDocker, runDockerInfo = origLookPath, origInfo }()
	lookPathDocker = func() error { return nil }
	runDockerInfo = func() (string, error) {
		return "connect: permission denied", errors.New("exit status 1")
	}

	if got := CheckDocker(); got != DockerPermissionDenied {
		t.Errorf("CheckDocker() = %v, want DockerPermissionDenied", got)
	}
}

func TestDockerStateMessageDistinctPerState(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range []DockerState{DockerOK, DockerBinaryMissing, DockerDaemonNotRunning, DockerPermissionDenied} {
		msg := s.Message()
		if msg == "" && s != DockerOK {
			t.Errorf("DockerState(%v).Message() = empty, want a message", s)
		}
		if seen[msg] && msg != "" {
			t.Errorf("DockerState(%v).Message() = %q, duplicate of another state's message", s, msg)
		}
		seen[msg] = true
	}
}
