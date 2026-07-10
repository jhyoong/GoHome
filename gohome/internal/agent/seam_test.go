package agent

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNoTUIImport enforces the architecture seam: none of the core packages
// (agent, llm, tools, guard, session) may transitively import internal/tui.
//
// The test shells out to `go list -deps <pkg>` for each package under scrutiny
// and asserts that none of the listed dependency paths contain
// "github.com/jhyoong/GoHome/gohome/internal/tui".
func TestNoTUIImport(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not found in PATH; skipping seam check")
	}

	const module = "github.com/jhyoong/GoHome"
	const tuiPkg = module + "/gohome/internal/tui"

	// Packages (and their subpackages) that must not import tui.
	pkgs := []string{
		module + "/gohome/internal/agent",
		module + "/gohome/internal/llm",
		module + "/gohome/internal/llm/anthropic",
		module + "/gohome/internal/llm/openai",
		module + "/gohome/internal/llm/common",
		module + "/gohome/internal/tools",
		module + "/gohome/internal/guard",
		module + "/gohome/internal/session",
	}

	for _, pkg := range pkgs {
		pkg := pkg // capture for parallel sub-tests
		t.Run(pkg, func(t *testing.T) {
			t.Parallel()
			out, err := exec.Command("go", "list", "-deps", pkg).Output()
			if err != nil {
				t.Fatalf("go list -deps %s: %v", pkg, err)
			}
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if strings.TrimSpace(line) == tuiPkg {
					t.Errorf("package %s transitively imports %s (seam violation)", pkg, tuiPkg)
				}
			}
		})
	}
}
