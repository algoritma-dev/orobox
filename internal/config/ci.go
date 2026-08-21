package config

// ReportsRelDir is where the machine-readable reports live, relative to the project root. It
// mirrors DeployArtifactsDir: everything Orobox generates for a run belongs under var/orobox.
//
// It lives here rather than in cmd because the generated GitLab pipeline has to name the very same
// paths the commands write to, and a second copy of them is how a generated pipeline starts
// publishing an artifact nothing produces.
const ReportsRelDir = "var/orobox/reports"

// RawReportsRelDir holds the per-tool report files exactly as the tools wrote them. They are kept
// rather than deleted after the merge: when a merged report looks wrong, the tool's own output is
// the only thing that says whether the tool or the merge is to blame.
const RawReportsRelDir = ReportsRelDir + "/raw"

// CIIncludeRelPath is the repo-relative path of the GitLab pipeline Orobox owns and rewrites on
// every ci-init, so job improvements reach existing projects without a manual merge.
const CIIncludeRelPath = ".gitlab-ci-orobox.yml"

// CIRootRelPath is the repo-relative path of the user-owned pipeline entry point. It is generated
// only when absent and does nothing but include CIIncludeRelPath.
const CIRootRelPath = ".gitlab-ci.yml"
