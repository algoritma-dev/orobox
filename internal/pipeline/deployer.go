package pipeline

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Deployer announces every task it starts on a line of its own, which is the only marker in the
// release step's output that says where a deploy has got to. Messenger::startTask writes
// "task <name>", prefixed with ::group:: under GitHub Actions. The GitLab form is a section
// marker with no task keyword, but the release container's environment is built by releaseEnv
// and carries no CI variables, so the plain form is what reaches us.
var deployerTaskLine = regexp.MustCompile(`^(?:::group::)?task\s+(\S+)\s*$`)

// parseTaskLine returns the task a Deployer announcement names.
func parseTaskLine(line string) (string, bool) {
	line = strings.TrimSpace(ansiCodes.ReplaceAllString(line, ""))
	match := deployerTaskLine.FindStringSubmatch(line)
	if match == nil {
		return "", false
	}
	return match[1], true
}

// summaryTasks is how many tasks the closing timing line names before the rest are bucketed. A
// deploy runs around twenty tasks and all but a few take milliseconds, so naming them all would
// bury the two that actually cost time.
const summaryTasks = 5

// deployerTracker turns the task announcements in the release step's output into the label a
// silence line needs and the timings the closing block prints. It never writes anything itself.
type deployerTracker struct {
	current   string
	startedAt time.Time
	// total accumulates per task, because a Deployer task can be announced more than once.
	total map[string]time.Duration
	// order remembers first appearance, so tasks of equal duration keep deploy order.
	order []string
}

func newDeployerTracker() *deployerTracker {
	return &deployerTracker{total: map[string]time.Duration{}}
}

// observe feeds one line of output to the tracker, closing the running task when the line
// announces a new one.
func (d *deployerTracker) observe(line string, now time.Time) {
	name, ok := parseTaskLine(line)
	if !ok {
		return
	}

	d.close(now)
	if _, seen := d.total[name]; !seen {
		d.total[name] = 0
		d.order = append(d.order, name)
	}
	d.current = name
	d.startedAt = now
}

// close books the running task's time. It is idempotent for a tracker with nothing running.
func (d *deployerTracker) close(now time.Time) {
	if d.current == "" {
		return
	}
	d.total[d.current] += now.Sub(d.startedAt)
	d.current = ""
}

func (d *deployerTracker) currentTask() string {
	if d == nil {
		return ""
	}
	return d.current
}

// summary is the one-line timing table printed when the release step ends: the longest tasks by
// name, the rest summed into a bucket.
func (d *deployerTracker) summary(now time.Time) string {
	if d == nil || len(d.order) == 0 {
		return ""
	}
	d.close(now)

	names := append([]string(nil), d.order...)
	sort.SliceStable(names, func(i, j int) bool { return d.total[names[i]] > d.total[names[j]] })

	var parts []string
	var others time.Duration
	for i, name := range names {
		if i < summaryTasks {
			parts = append(parts, fmt.Sprintf("%s %s", name, humanDuration(d.total[name])))
			continue
		}
		others += d.total[name]
	}
	if len(names) > summaryTasks {
		parts = append(parts, fmt.Sprintf("others %s", humanDuration(others)))
	}
	return strings.Join(parts, " · ")
}

// humanDuration renders a duration the way the progress lines do: seconds below a minute,
// minutes and zero-padded seconds above it.
func humanDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}
