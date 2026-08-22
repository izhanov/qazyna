package cli

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"qazyna/internal/skill"
)

const releaseRepo = "izhanov/qazyna"

// releaseVersionRe matches a clean release build (v0.2.0). Source builds
// carry a git-describe suffix (v0.1.0-9-gabc123-dirty) or "dev".
var releaseVersionRe = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// updateAction replaces the running binary with the latest GitHub release
// and refreshes the installed Claude Code skill — it is versioned together
// with the binary, so the two must move in step.
func (a *App) updateAction(ctx context.Context, cmd *cli.Command) error {
	setupLogging(cmd)

	latest, err := latestReleaseTag(ctx)
	if err != nil {
		return err
	}

	if cmd.Bool("check") {
		if latest == version {
			fmt.Printf("up to date: %s\n", version)
		} else {
			fmt.Printf("current %s, latest %s — run `qazyna update`\n", version, latest)
		}
		return nil
	}

	if !releaseVersionRe.MatchString(version) {
		return fmt.Errorf("this is a source build (%s) — update it with `make install`, "+
			"or switch to releases:  curl -fsSL https://raw.githubusercontent.com/%s/main/install.sh | sh",
			version, releaseRepo)
	}
	if latest == version {
		fmt.Printf("already up to date: %s\n", version)
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}

	fmt.Printf("updating %s → %s\n", version, latest)

	// The temp dir lives next to the binary so the final rename is atomic
	// (same filesystem) — the running binary is never half-replaced.
	tmp, err := os.MkdirTemp(filepath.Dir(exe), ".qazyna-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	binPath := filepath.Join(tmp, "qazyna")
	if err := downloadRelease(ctx, latest, binPath); err != nil {
		return err
	}

	out, err := exec.CommandContext(ctx, binPath, "--version").Output()
	if err != nil {
		return fmt.Errorf("downloaded binary failed to run: %w", err)
	}
	if err := os.Rename(binPath, exe); err != nil {
		return err
	}
	fmt.Printf("updated: %s (%s)\n", exe, strings.TrimSpace(string(out)))

	if path, err := skill.Install(); err != nil {
		fmt.Printf("warning: could not refresh the Claude Code skill: %v\n", err)
	} else {
		fmt.Printf("skill refreshed: %s\n", path)
	}
	return nil
}

// latestReleaseTag asks the GitHub API for the newest release tag.
func latestReleaseTag(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	url := "https://api.github.com/repos/" + releaseRepo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases: HTTP %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", fmt.Errorf("github releases: latest release has no tag")
	}
	return release.TagName, nil
}

// downloadRelease fetches the release asset for this platform and extracts
// the binary into dest.
func downloadRelease(ctx context.Context, tag, dest string) error {
	asset := fmt.Sprintf("qazyna_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", releaseRepo, tag, asset)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d (no build for %s/%s?)",
			url, resp.StatusCode, runtime.GOOS, runtime.GOARCH)
	}

	return extractBinary(resp.Body, dest)
}

// extractBinary pulls the qazyna binary out of a release tar.gz stream.
func extractBinary(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != "qazyna" {
			continue
		}
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil { //nolint:gosec // release archives are our own
			f.Close()
			return err
		}
		return f.Close()
	}
	return fmt.Errorf("qazyna binary not found in the release archive")
}
