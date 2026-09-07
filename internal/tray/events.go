package tray

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
)

// DockerEvent is the subset of `docker events --format {{json .}}` fields the tray cares about.
type DockerEvent struct {
	Type   string `json:"Type"`
	Action string `json:"Action"`
	Actor  struct {
		Attributes map[string]string `json:"Attributes"`
	} `json:"Actor"`
}

// ParseDockerEventLine decodes one line of `docker events --format {{json .}}` output. A line
// that fails to parse is reported via ok=false, the same "skip it" treatment
// ParseServiceStatuses gives malformed `ps` output.
func ParseDockerEventLine(line []byte) (DockerEvent, bool) {
	var evt DockerEvent
	if err := json.Unmarshal(line, &evt); err != nil {
		return DockerEvent{}, false
	}
	return evt, true
}

// dockerEventsCmd is a variable so tests could replace the spawned command; filtered to
// containers carrying compose's own project label, so events from unrelated containers on the
// host never wake the tray.
var dockerEventsCmd = func(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(ctx, "docker", "events", "--filter", "label=com.docker.compose.project", "--format", "{{json .}}")
}

// StreamDockerEvents runs `docker events` and calls onEvent for each decoded event, until ctx is
// canceled or the docker events process itself exits (daemon restart, network hiccup, ...). A
// natural exit returns nil rather than an error: the caller's job is to notice the stream ended
// and fall back to polling (§6.4 v2), not to treat that as fatal.
func StreamDockerEvents(ctx context.Context, onEvent func(DockerEvent)) error {
	cmd := dockerEventsCmd(ctx)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if evt, ok := ParseDockerEventLine(scanner.Bytes()); ok {
			onEvent(evt)
		}
	}

	return cmd.Wait()
}
