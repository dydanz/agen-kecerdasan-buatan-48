package onboard

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// Run detects claude CLI auth state and prints config guidance.
func Run() {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		fmt.Println("claude binary not found in PATH.")
		fmt.Println("Install Claude Code CLI: https://claude.ai/download")
		fmt.Println("Then run: akb48 --onboard")
		return
	}
	fmt.Printf("Found claude at %s\n\n", claudePath)

	out, err := exec.Command(claudePath, "auth", "status", "--json").Output() //nolint:gosec
	if err != nil {
		fmt.Println("Not authenticated.")
		fmt.Println("Run: claude auth login")
		return
	}

	var status struct {
		LoggedIn bool   `json:"loggedIn"`
		Email    string `json:"email"`
	}
	_ = json.Unmarshal(out, &status)

	if !status.LoggedIn {
		fmt.Println("Not authenticated.")
		fmt.Println("Run: claude auth login")
		return
	}

	fmt.Printf("Authenticated as: %s\n\n", status.Email)
	fmt.Println("Add to config.toml under [llm]:")
	fmt.Println()
	fmt.Println(`  backend = "claude-cli"`)
	fmt.Println()
	fmt.Println("For a long-lived scripted token (recommended):")
	fmt.Println("  claude setup-token")
	fmt.Println("  export CLAUDE_CODE_OAUTH_TOKEN=<token printed above>")
	fmt.Println()
	fmt.Println("Then start normally: akb48")
}
