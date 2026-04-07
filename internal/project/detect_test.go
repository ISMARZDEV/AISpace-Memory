package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ─── extractRepoName ──────────────────────────────────────────────────────────

func TestExtractRepoName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"git@github.com:user/my-cool-repo.git", "my-cool-repo"},
		{"https://github.com/user/my-cool-repo.git", "my-cool-repo"},
		{"https://github.com/user/my-cool-repo", "my-cool-repo"},
		{"git@github.com:org/repo", "repo"},
		{"https://gitlab.com/group/subgroup/project.git", "project"},
		{"ssh://git@bitbucket.org/team/service.git", "service"},
		{"", ""},
		{".git", ""},
	}
	for _, tc := range cases {
		got := extractRepoName(tc.input)
		if got != tc.want {
			t.Errorf("extractRepoName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ─── git-based detection (integration) ───────────────────────────────────────

func TestDetectProject_GitRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "remote", "add", "origin", "git@github.com:user/my-cool-repo.git")

	got := DetectProject(dir)
	if got != "my-cool-repo" {
		t.Errorf("got %q, want %q", got, "my-cool-repo")
	}
}

func TestDetectProject_GitRemote_HTTPS(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "remote", "add", "origin", "https://github.com/user/aispace-memory.git")

	got := DetectProject(dir)
	if got != "aispace-memory" {
		t.Errorf("got %q, want %q", got, "aispace-memory")
	}
}

func TestDetectProject_GitRootNoRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")

	got := DetectProject(dir)
	want := normalize(filepath.Base(dir))
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDetectProject_NonGitDir(t *testing.T) {
	dir := t.TempDir()
	got := DetectProject(dir)
	want := normalize(filepath.Base(dir))
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDetectProject_EmptyDir_NoPanic(t *testing.T) {
	got := DetectProject("")
	if got == "" {
		t.Error("expected non-empty result for empty dir")
	}
}

func TestDetectProject_NormalizedLowercase(t *testing.T) {
	dir := t.TempDir()
	got := DetectProject(dir)
	if got != normalize(got) {
		t.Errorf("result %q is not normalized", got)
	}
}

// ─── .aispace-men.json (highest priority) ────────────────────────────────────

func TestDetectProject_ConfigFile_ExplicitProject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".aispace-men.json"), `{"project": "my-pinned-project"}`)

	got := DetectProject(dir)
	if got != "my-pinned-project" {
		t.Errorf("got %q, want %q", got, "my-pinned-project")
	}
}

func TestDetectProject_ConfigFile_WalksUp(t *testing.T) {
	// .aispace-men.json is in the parent, detect from the child
	parent := t.TempDir()
	child := filepath.Join(parent, "subdir")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(parent, ".aispace-men.json"), `{"project": "monorepo-root"}`)

	got := DetectProject(child)
	if got != "monorepo-root" {
		t.Errorf("got %q, want %q", got, "monorepo-root")
	}
}

func TestDetectProject_ConfigFile_NormalizesName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".aispace-men.json"), `{"project": "  My Project  "}`)

	got := DetectProject(dir)
	if got != "my project" {
		t.Errorf("got %q, want %q", got, "my project")
	}
}

func TestDetectProject_ConfigFile_BeatsGitRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "remote", "add", "origin", "git@github.com:user/git-name.git")
	// Config file must win over git remote
	writeFile(t, filepath.Join(dir, ".aispace-men.json"), `{"project": "config-wins"}`)

	got := DetectProject(dir)
	if got != "config-wins" {
		t.Errorf("got %q, want %q (config file should beat git remote)", got, "config-wins")
	}
}

// ─── go.mod ───────────────────────────────────────────────────────────────────

func TestDetectProject_GoMod(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/myorg/my-service\n\ngo 1.21\n")

	got := DetectProject(dir)
	if got != "my-service" {
		t.Errorf("got %q, want %q", got, "my-service")
	}
}

func TestDetectProject_GoMod_SimpleModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module my-cli\n\ngo 1.22\n")

	got := DetectProject(dir)
	if got != "my-cli" {
		t.Errorf("got %q, want %q", got, "my-cli")
	}
}

func TestDetectProject_GoMod_WalksUp(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "cmd", "server")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(parent, "go.mod"), "module github.com/org/backend\n")

	got := DetectProject(child)
	if got != "backend" {
		t.Errorf("got %q, want %q", got, "backend")
	}
}

// ─── package.json ─────────────────────────────────────────────────────────────

func TestDetectProject_PackageJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"name": "my-frontend-app", "version": "1.0.0"}`)

	got := DetectProject(dir)
	if got != "my-frontend-app" {
		t.Errorf("got %q, want %q", got, "my-frontend-app")
	}
}

func TestDetectProject_PackageJSON_ScopedPackage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"name": "@myorg/my-lib"}`)

	got := DetectProject(dir)
	if got != "my-lib" {
		t.Errorf("got %q, want %q (scoped package should strip @org/)", got, "my-lib")
	}
}

// ─── Cargo.toml ──────────────────────────────────────────────────────────────

func TestDetectProject_CargoToml(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Cargo.toml"), "[package]\nname = \"my-rust-cli\"\nversion = \"0.1.0\"\n")

	got := DetectProject(dir)
	if got != "my-rust-cli" {
		t.Errorf("got %q, want %q", got, "my-rust-cli")
	}
}

// ─── pyproject.toml ───────────────────────────────────────────────────────────

func TestDetectProject_Pyproject_PEP518(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pyproject.toml"), "[project]\nname = \"my-python-lib\"\nversion = \"0.1.0\"\n")

	got := DetectProject(dir)
	if got != "my-python-lib" {
		t.Errorf("got %q, want %q", got, "my-python-lib")
	}
}

func TestDetectProject_Pyproject_Poetry(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pyproject.toml"), "[tool.poetry]\nname = \"my-poetry-app\"\nversion = \"1.0.0\"\n")

	got := DetectProject(dir)
	if got != "my-poetry-app" {
		t.Errorf("got %q, want %q", got, "my-poetry-app")
	}
}

// ─── Manifest priority: go.mod beats package.json ────────────────────────────

func TestDetectProject_GoModBeatsPackageJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/org/go-service\n")
	writeFile(t, filepath.Join(dir, "package.json"), `{"name": "js-package"}`)

	got := DetectProject(dir)
	if got != "go-service" {
		t.Errorf("got %q, want %q (go.mod should beat package.json)", got, "go-service")
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("command %q failed: %v\n%s", name+" "+args[0], err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
