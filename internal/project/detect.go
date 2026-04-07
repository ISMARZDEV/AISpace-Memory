// Package project provides utilities for detecting and normalizing project names.
//
// It replicates the detection logic from the Claude Code shell helpers and
// OpenCode TypeScript plugin in pure Go, so CLI and MCP server can share
// a single canonical implementation.
package project

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DetectProject detects the project name for a given directory.
//
// Priority chain (first non-empty result wins):
//  1. .aispace-men.json  → explicit "project" field set by the user
//  2. git remote origin  → repo name extracted from the URL
//  3. go.mod             → module name (last path segment)
//  4. package.json       → "name" field
//  5. Cargo.toml         → [package] name field
//  6. pyproject.toml     → [project] or [tool.poetry] name field
//  7. git root basename  → name of the repository root directory
//  8. dir basename       → fallback to the directory name
//
// The returned name is always non-empty and already normalized (lowercase, trimmed).
func DetectProject(dir string) string {
	// Guard empty dir — nothing useful to detect.
	if dir == "" {
		return "unknown"
	}
	// Guard against arg injection: a dir starting with "-" would be
	// interpreted as a git flag when passed to `git -C <dir>`.
	if strings.HasPrefix(dir, "-") {
		dir = "./" + dir
	}

	// 1. Explicit config file — highest priority (user intent is unambiguous).
	if name := detectFromConfigFile(dir); name != "" {
		return normalize(name)
	}

	// 2. Git remote origin — most reliable for hosted projects.
	if name := detectFromGitRemote(dir); name != "" {
		return normalize(name)
	}

	// 3-6. Language manifest files — walk up from dir to filesystem root.
	if name := detectFromManifest(dir); name != "" {
		return normalize(name)
	}

	// 7. Git root basename — reliable for local-only repos.
	if name := detectFromGitRoot(dir); name != "" {
		return normalize(name)
	}

	// 8. Dir basename — last resort.
	base := filepath.Base(dir)
	if base == "" || base == "." {
		return "unknown"
	}
	return normalize(base)
}

// normalize applies canonical project name rules: lowercase + trim whitespace.
// It mirrors the normalization applied by the store layer so that DetectProject
// always returns a value that is consistent with stored project names.
func normalize(name string) string {
	n := strings.TrimSpace(strings.ToLower(name))
	if n == "" {
		return "unknown"
	}
	return n
}

// ─── Source 1: .aispace-men.json ─────────────────────────────────────────────

// detectFromConfigFile reads the nearest .aispace-men.json walking up from dir.
// This lets users pin an explicit project name that overrides all auto-detection.
//
//	{ "project": "my-monorepo" }
func detectFromConfigFile(dir string) string {
	for {
		path := filepath.Join(dir, ".aispace-men.json")
		data, err := os.ReadFile(path)
		if err == nil {
			var cfg struct {
				Project string `json:"project"`
			}
			if json.Unmarshal(data, &cfg) == nil && cfg.Project != "" {
				return cfg.Project
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}
	return ""
}

// ─── Source 2: git remote origin ─────────────────────────────────────────────

// detectFromGitRemote attempts to determine the project name from the git
// remote "origin" URL. Returns empty string if git is unavailable, the
// directory is not a repo, or there is no origin remote.
func detectFromGitRemote(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(out))
	return extractRepoName(url)
}

// ─── Sources 3-6: Language manifest files ────────────────────────────────────

// detectFromManifest walks up the directory tree looking for known project
// manifest files and extracts the project name from the first one found.
//
// Supported manifests (checked in order per directory level):
//   - go.mod        (Go)
//   - package.json  (Node / Deno / Bun)
//   - Cargo.toml    (Rust)
//   - pyproject.toml (Python — PEP 517/518 and Poetry)
func detectFromManifest(dir string) string {
	for {
		if name := readGoMod(dir); name != "" {
			return name
		}
		if name := readPackageJSON(dir); name != "" {
			return name
		}
		if name := readCargoToml(dir); name != "" {
			return name
		}
		if name := readPyprojectToml(dir); name != "" {
			return name
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// readGoMod extracts the last path segment of the module path from go.mod.
//
//	module github.com/org/my-service → "my-service"
func readGoMod(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		mod := strings.TrimSpace(strings.TrimPrefix(line, "module "))
		// Remove any inline comment
		if idx := strings.Index(mod, "//"); idx != -1 {
			mod = strings.TrimSpace(mod[:idx])
		}
		// Take only the last segment of the module path
		parts := strings.Split(mod, "/")
		name := parts[len(parts)-1]
		if name != "" {
			return name
		}
	}
	return ""
}

// readPackageJSON extracts the "name" field from package.json.
// Scoped packages (@org/name) return just the short name.
func readPackageJSON(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return ""
	}
	var pkg struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(data, &pkg) != nil || pkg.Name == "" {
		return ""
	}
	// Strip scope prefix: "@org/my-package" → "my-package"
	name := pkg.Name
	if idx := strings.LastIndex(name, "/"); idx != -1 {
		name = name[idx+1:]
	}
	return name
}

// readCargoToml extracts the package name from Cargo.toml.
// Uses simple line scanning to avoid a TOML parser dependency.
func readCargoToml(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "Cargo.toml"))
	if err != nil {
		return ""
	}
	inPackage := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[package]" {
			inPackage = true
			continue
		}
		// Stop at next section header
		if inPackage && strings.HasPrefix(trimmed, "[") {
			break
		}
		if inPackage && strings.HasPrefix(trimmed, "name") {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 {
				name := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
				if name != "" {
					return name
				}
			}
		}
	}
	return ""
}

// readPyprojectToml extracts the package name from pyproject.toml.
// Supports both PEP 517/518 [project] and Poetry [tool.poetry] tables.
func readPyprojectToml(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "pyproject.toml"))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")

	// Two-pass: first look for [project] name, then [tool.poetry] name.
	for _, section := range []string{"[project]", "[tool.poetry]"} {
		inSection := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == section {
				inSection = true
				continue
			}
			if inSection && strings.HasPrefix(trimmed, "[") {
				break
			}
			if inSection && strings.HasPrefix(trimmed, "name") {
				parts := strings.SplitN(trimmed, "=", 2)
				if len(parts) == 2 {
					name := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
					if name != "" {
						return name
					}
				}
			}
		}
	}
	return ""
}

// ─── Source 7: git root basename ─────────────────────────────────────────────

// detectFromGitRoot returns the basename of the git repository root.
// Falls back to empty string when git is unavailable or the directory is not
// inside a git repository.
func detectFromGitRoot(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return ""
	}
	return filepath.Base(root)
}

// ─── URL parsing ─────────────────────────────────────────────────────────────

// extractRepoName parses a git remote URL and returns just the repository name.
//
// Supported URL formats:
//   - SSH:   git@github.com:user/repo.git
//   - HTTPS: https://github.com/user/repo.git
//   - Either with or without the trailing .git suffix
func extractRepoName(url string) string {
	// Strip trailing .git suffix
	url = strings.TrimSuffix(url, ".git")

	// Split on both "/" and ":" to handle SSH and HTTPS uniformly
	parts := strings.FieldsFunc(url, func(r rune) bool {
		return r == '/' || r == ':'
	})
	if len(parts) == 0 {
		return ""
	}
	name := parts[len(parts)-1]
	return strings.TrimSpace(name)
}
