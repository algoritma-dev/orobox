package tray

import "testing"

func TestParseDockerEventLineDecodesRealisticLine(t *testing.T) {
	line := []byte(`{"status":"start","Type":"container","Action":"start","Actor":{"ID":"abc123","Attributes":{"com.docker.compose.project":"mybundle","com.docker.compose.service":"web"}}}`)

	evt, ok := ParseDockerEventLine(line)
	if !ok {
		t.Fatal("ParseDockerEventLine() ok = false, want true for a well-formed line")
	}
	if evt.Type != "container" || evt.Action != "start" {
		t.Errorf("ParseDockerEventLine() = %+v, want Type=container Action=start", evt)
	}
	if evt.Actor.Attributes["com.docker.compose.project"] != "mybundle" {
		t.Errorf("ParseDockerEventLine() Actor.Attributes = %+v, want project=mybundle", evt.Actor.Attributes)
	}
}

func TestParseDockerEventLineMalformedReturnsFalse(t *testing.T) {
	if _, ok := ParseDockerEventLine([]byte("not json")); ok {
		t.Error("ParseDockerEventLine() ok = true, want false for malformed input")
	}
}
