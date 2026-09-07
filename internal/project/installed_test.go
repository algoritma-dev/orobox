package project

import (
	"errors"
	"testing"
)

func TestIsInstalledTrueWhenQueryReturnsOne(t *testing.T) {
	orig := runDBQuery
	defer func() { runDBQuery = orig }()
	runDBQuery = func(args []string) ([]byte, error) { return []byte("1\n"), nil }

	p := Project{Name: "mybundle", InternalDir: t.TempDir()}
	installed, err := p.IsInstalled()
	if err != nil || !installed {
		t.Errorf("IsInstalled() = (%v, %v), want (true, nil)", installed, err)
	}
}

func TestIsInstalledFalseWhenQueryReturnsZero(t *testing.T) {
	orig := runDBQuery
	defer func() { runDBQuery = orig }()
	runDBQuery = func(args []string) ([]byte, error) { return []byte("0\n"), nil }

	p := Project{Name: "mybundle", InternalDir: t.TempDir()}
	installed, err := p.IsInstalled()
	if err != nil || installed {
		t.Errorf("IsInstalled() = (%v, %v), want (false, nil)", installed, err)
	}
}

func TestIsInstalledFalseWhenTableDoesNotExist(t *testing.T) {
	orig := runDBQuery
	defer func() { runDBQuery = orig }()
	runDBQuery = func(args []string) ([]byte, error) {
		return []byte(`ERROR:  relation "oro_config_value" does not exist`), errors.New("exit status 1")
	}

	p := Project{Name: "mybundle", InternalDir: t.TempDir()}
	installed, err := p.IsInstalled()
	if err != nil || installed {
		t.Errorf("IsInstalled() = (%v, %v), want (false, nil) when the table does not exist yet", installed, err)
	}
}

func TestIsInstalledFalseWhenDatabaseDoesNotExist(t *testing.T) {
	orig := runDBQuery
	defer func() { runDBQuery = orig }()
	runDBQuery = func(args []string) ([]byte, error) {
		return []byte(`psql: error: FATAL:  database "oro_db" does not exist`), errors.New("exit status 2")
	}

	p := Project{Name: "mybundle", InternalDir: t.TempDir()}
	installed, err := p.IsInstalled()
	if err != nil || installed {
		t.Errorf("IsInstalled() = (%v, %v), want (false, nil) when the database does not exist yet", installed, err)
	}
}

func TestIsInstalledPropagatesUnknownError(t *testing.T) {
	orig := runDBQuery
	defer func() { runDBQuery = orig }()
	runDBQuery = func(args []string) ([]byte, error) {
		return []byte("connection refused"), errors.New("exit status 1")
	}

	p := Project{Name: "mybundle", InternalDir: t.TempDir()}
	if _, err := p.IsInstalled(); err == nil {
		t.Fatal("IsInstalled() error = nil, want error for an unrecognized failure")
	}
}
