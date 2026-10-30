package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FetchOptions controls a sparse git checkout of an upstream documentation
// repository.
type FetchOptions struct {
	Dir      string
	Upstream string
	Ref      string // commit, branch or tag; empty means the remote default
	Select   []string
	Force    bool
	Log      io.Writer
}

// Fetch creates or updates a sparse checkout and returns the checked-out
// commit. The snapshot directory is managed by docty: reusing a directory that
// belongs to another repository is refused unless Force is set.
func Fetch(ctx context.Context, o FetchOptions) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", errors.New("git no está instalado; instálalo o usa: docty index js --from <ruta>")
	}
	if o.Upstream == "" {
		return "", errors.New("el manifest no define upstream")
	}
	paths := sparsePaths(o.Select)
	if len(paths) == 0 {
		return "", errors.New("el manifest no define rutas select")
	}

	if o.Force {
		if err := os.RemoveAll(o.Dir); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return "", err
	}

	hasGit := false
	if st, err := os.Stat(filepath.Join(o.Dir, ".git")); err == nil && st.IsDir() {
		hasGit = true
	}
	if hasGit {
		url, err := git(ctx, o.Dir, "remote", "get-url", "origin")
		if err != nil || strings.TrimSpace(url) != o.Upstream {
			return "", fmt.Errorf("el directorio %s ya existe y no es un snapshot de %s; usa --force", o.Dir, o.Upstream)
		}
		// Re-running update with the same pinned commit should not download
		// anything; only reindex (the caller always rebuilds).
		if len(o.Ref) == 40 {
			if head, err := git(ctx, o.Dir, "rev-parse", "HEAD"); err == nil && strings.TrimSpace(head) == o.Ref {
				return o.Ref, nil
			}
		}
	} else {
		if entries, err := os.ReadDir(o.Dir); err == nil && len(entries) > 0 {
			return "", fmt.Errorf("el directorio %s no está vacío y no es un repo git; usa --force", o.Dir)
		}
		if _, err := git(ctx, o.Dir, "init", "-q"); err != nil {
			return "", err
		}
		if _, err := git(ctx, o.Dir, "remote", "add", "origin", o.Upstream); err != nil {
			return "", err
		}
	}

	if _, err := git(ctx, o.Dir, "sparse-checkout", "init", "--cone"); err != nil {
		return "", err
	}
	setArgs := append([]string{"sparse-checkout", "set"}, paths...)
	if _, err := git(ctx, o.Dir, setArgs...); err != nil {
		return "", err
	}

	fetchArgs := []string{"fetch", "--depth", "1", "--no-tags", "origin"}
	if o.Ref != "" {
		fetchArgs = append(fetchArgs, o.Ref)
	}
	if o.Log != nil {
		ref := o.Ref
		if ref == "" {
			ref = "HEAD"
		}
		fmt.Fprintf(o.Log, "  git fetch %s (%s)\n", ref, o.Upstream)
	}
	if _, err := git(ctx, o.Dir, fetchArgs...); err != nil {
		return "", fmt.Errorf("descargando el snapshot: %w", err)
	}
	if _, err := git(ctx, o.Dir, "checkout", "-q", "--force", "FETCH_HEAD"); err != nil {
		return "", fmt.Errorf("preparando el snapshot: %w", err)
	}

	sha, err := git(ctx, o.Dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}

// sparsePaths turns manifest globs into cone-mode directories.
func sparsePaths(selects []string) []string {
	var out []string
	for _, s := range selects {
		s = strings.TrimSpace(s)
		s = strings.TrimSuffix(s, "/**")
		s = strings.TrimSuffix(s, "/*")
		s = strings.TrimSuffix(s, "/")
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}
