package tray

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveOroboxBinPrefersOroboxBinEnv(t *testing.T) {
	t.Setenv("OROBOX_BIN", "/custom/path/orobox")
	t.Setenv("PATH", t.TempDir()) // must not matter: env wins even if PATH has nothing

	got, err := ResolveOroboxBin()
	if err != nil {
		t.Fatalf("ResolveOroboxBin() error = %v", err)
	}
	if got != "/custom/path/orobox" {
		t.Errorf("ResolveOroboxBin() = %q, want %q", got, "/custom/path/orobox")
	}
}

func TestResolveOroboxBinFallsBackToPath(t *testing.T) {
	t.Setenv("OROBOX_BIN", "")
	dir := t.TempDir()
	binPath := filepath.Join(dir, "orobox")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	t.Setenv("PATH", dir)

	got, err := ResolveOroboxBin()
	if err != nil {
		t.Fatalf("ResolveOroboxBin() error = %v", err)
	}
	if got != binPath {
		t.Errorf("ResolveOroboxBin() = %q, want %q", got, binPath)
	}
}

func TestResolveOroboxBinErrorsWhenNotFound(t *testing.T) {
	t.Setenv("OROBOX_BIN", "")
	t.Setenv("PATH", t.TempDir()) // empty, no orobox binary anywhere
	t.Setenv("HOME", t.TempDir()) // neutralize the fallback candidate dirs below too

	if _, err := ResolveOroboxBin(); err == nil {
		t.Fatal("ResolveOroboxBin() error = nil, want error when orobox is nowhere to be found")
	}
}

// A binary launched from a graphical session (GNOME Activities, systemd --user, a .desktop
// Exec=) does not inherit the PATH additions a login shell's rc file makes (e.g. `go install`'s
// $HOME/go/bin), which is exactly how `go install ...orobox@latest` — the install method this
// repo's own README recommends — leaves the CLI unreachable from a desktop-launched
// orobox-tray. These fallbacks are checked after a bare PATH lookup fails.
func TestResolveOroboxBinFallsBackToGoInstallDir(t *testing.T) {
	t.Setenv("OROBOX_BIN", "")
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)

	goBinDir := filepath.Join(home, "go", "bin")
	if err := os.MkdirAll(goBinDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	binPath := filepath.Join(goBinDir, "orobox")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	got, err := ResolveOroboxBin()
	if err != nil {
		t.Fatalf("ResolveOroboxBin() error = %v", err)
	}
	if got != binPath {
		t.Errorf("ResolveOroboxBin() = %q, want %q", got, binPath)
	}
}

func TestResolveOroboxBinFallsBackToLocalBinDir(t *testing.T) {
	t.Setenv("OROBOX_BIN", "")
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)

	localBinDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(localBinDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	binPath := filepath.Join(localBinDir, "orobox")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	got, err := ResolveOroboxBin()
	if err != nil {
		t.Fatalf("ResolveOroboxBin() error = %v", err)
	}
	if got != binPath {
		t.Errorf("ResolveOroboxBin() = %q, want %q", got, binPath)
	}
}

func TestResolveOroboxBinPathTakesPriorityOverFallbacks(t *testing.T) {
	t.Setenv("OROBOX_BIN", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	// A real one on PATH...
	pathDir := t.TempDir()
	pathBin := filepath.Join(pathDir, "orobox")
	if err := os.WriteFile(pathBin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	t.Setenv("PATH", pathDir)

	// ...and a decoy in the fallback dir, which must lose.
	goBinDir := filepath.Join(home, "go", "bin")
	if err := os.MkdirAll(goBinDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goBinDir, "orobox"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write decoy binary: %v", err)
	}

	got, err := ResolveOroboxBin()
	if err != nil {
		t.Fatalf("ResolveOroboxBin() error = %v", err)
	}
	if got != pathBin {
		t.Errorf("ResolveOroboxBin() = %q, want the PATH entry %q, not a fallback", got, pathBin)
	}
}

// Regression test: a stray non-binary file (observed: a gzip download left behind by a failed
// install) sitting executable in a fallback dir must not be handed to exec.Command — that fails
// at run time with "exec format error" well after ResolveOroboxBin claimed success. The +x bit
// alone does not mean the file is a real binary or script.
func TestResolveOroboxBinSkipsCorruptFileInFallbackDir(t *testing.T) {
	t.Setenv("OROBOX_BIN", "")
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)

	// A corrupt file (gzip magic bytes, not ELF or a #! script) with the executable bit set,
	// in the first fallback dir checked.
	goBinDir := filepath.Join(home, "go", "bin")
	if err := os.MkdirAll(goBinDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goBinDir, "orobox"), []byte{0x1f, 0x8b, 0x08, 0x00}, 0755); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	// A real one in the next fallback dir, which must be found instead.
	localBinDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(localBinDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	realBin := filepath.Join(localBinDir, "orobox")
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write real binary: %v", err)
	}

	got, err := ResolveOroboxBin()
	if err != nil {
		t.Fatalf("ResolveOroboxBin() error = %v", err)
	}
	if got != realBin {
		t.Errorf("ResolveOroboxBin() = %q, want %q (the corrupt file must be skipped)", got, realBin)
	}
}

func TestResolveOroboxBinAcceptsELFBinary(t *testing.T) {
	t.Setenv("OROBOX_BIN", "")
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)

	goBinDir := filepath.Join(home, "go", "bin")
	if err := os.MkdirAll(goBinDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	binPath := filepath.Join(goBinDir, "orobox")
	elfMagic := []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0}
	if err := os.WriteFile(binPath, elfMagic, 0755); err != nil {
		t.Fatalf("write fake ELF binary: %v", err)
	}

	got, err := ResolveOroboxBin()
	if err != nil {
		t.Fatalf("ResolveOroboxBin() error = %v", err)
	}
	if got != binPath {
		t.Errorf("ResolveOroboxBin() = %q, want %q", got, binPath)
	}
}
