package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"qazyna/internal/skill"
)

// setupAction prepares everything qazyna needs in one command: checks
// Ollama and the embedding model, checks poppler, registers the MCP server
// in Claude Code and installs the agent skill. Best-effort by design —
// steps it cannot do itself (brew install ...) become printed hints.
func (a *App) setupAction(ctx context.Context, cmd *cli.Command) error {
	cfg := newConfig(cmd)

	if _, err := exec.LookPath("ollama"); err != nil {
		fail("ollama not found")
		hint("install it:  brew install ollama    # or: curl -fsSL https://ollama.com/install.sh | sh")
		hint("then re-run: qazyna setup")
	} else {
		pass("ollama found")
		setupModel(ctx, cfg.OllamaURL, cfg.EmbedModel)
	}

	if _, err := exec.LookPath("pdftotext"); err != nil {
		fail("pdftotext not found — PDF files will be skipped")
		hint("install poppler:  brew install poppler    # or: apt-get install poppler-utils")
	} else {
		pass("poppler found (pdftotext)")
	}

	setupMCP(ctx)

	if path, err := skill.Install(); err != nil {
		fail(fmt.Sprintf("Claude Code skill: %v", err))
	} else {
		pass("Claude Code skill installed: " + path)
	}

	fmt.Println("\nnext: qazyna index <path to your notes or docs>")
	return nil
}

// setupModel checks that the Ollama server is reachable and the embedding
// model is present, pulling it if needed.
func setupModel(ctx context.Context, ollamaURL, model string) {
	models, err := installedModels(ctx, ollamaURL)
	if err != nil {
		fail("ollama server not reachable at " + ollamaURL)
		hint("start it:  ollama serve    # then re-run: qazyna setup")
		return
	}
	if hasModel(models, model) {
		pass("embedding model " + model + " present")
		return
	}

	fmt.Printf("… pulling embedding model %s\n", model)
	pull := exec.CommandContext(ctx, "ollama", "pull", model)
	pull.Stdout = os.Stdout
	pull.Stderr = os.Stderr
	if err := pull.Run(); err != nil {
		fail(fmt.Sprintf("ollama pull %s: %v", model, err))
		return
	}
	pass("embedding model " + model + " pulled")
}

// setupMCP registers this binary as an MCP server in Claude Code.
func setupMCP(ctx context.Context) {
	if _, err := exec.LookPath("claude"); err != nil {
		fail("claude CLI not found — skipping MCP registration")
		hint("install Claude Code, then run:  claude mcp add qazyna -- qazyna mcp")
		return
	}

	exe, err := os.Executable()
	if err != nil {
		fail(fmt.Sprintf("cannot locate own binary: %v", err))
		return
	}

	out, err := exec.CommandContext(ctx, "claude", "mcp", "add", "qazyna", "--", exe, "mcp").CombinedOutput()
	switch {
	case err == nil:
		pass("MCP server registered in Claude Code")
	case strings.Contains(string(out), "already exists"):
		pass("MCP server already registered in Claude Code")
	default:
		fail(fmt.Sprintf("claude mcp add: %v", err))
		hint(strings.TrimSpace(string(out)))
	}
}

// installedModels asks the Ollama server which models it has.
func installedModels(ctx context.Context, ollamaURL string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(ollamaURL, "/")+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama: HTTP %d", resp.StatusCode)
	}

	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		names = append(names, m.Name)
	}
	return names, nil
}

// hasModel reports whether the model is present, tolerating tag suffixes:
// "bge-m3" matches both "bge-m3" and "bge-m3:latest".
func hasModel(models []string, want string) bool {
	for _, m := range models {
		if m == want || strings.TrimSuffix(m, ":latest") == want || m == want+":latest" {
			return true
		}
	}
	return false
}

func pass(msg string) { fmt.Println("✓ " + msg) }
func fail(msg string) { fmt.Println("✗ " + msg) }
func hint(msg string) { fmt.Println("  → " + msg) }
