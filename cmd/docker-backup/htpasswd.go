package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

var htpasswdCmd = &cobra.Command{
	Use:   "htpasswd <username>",
	Short: "Generate htpasswd-style password hash",
	Long: `Generate a bcrypt password hash for use with --dashboard.auth.basic.

Examples:
  # Interactive password prompt
  docker-backup htpasswd admin

  # Pipe password from stdin
  echo "mypassword" | docker-backup htpasswd admin

  # Append to htpasswd file
  docker-backup htpasswd admin >> /etc/docker-backup/htpasswd`,
	Args: cobra.ExactArgs(1),
	RunE: runHtpasswd,
}

var htpasswdCost int

func init() {
	htpasswdCmd.Flags().IntVarP(&htpasswdCost, "cost", "c", bcrypt.DefaultCost, "bcrypt cost factor (4-31, higher is slower but more secure)")
	rootCmd.AddCommand(htpasswdCmd)
}

func runHtpasswd(cmd *cobra.Command, args []string) error {
	username := args[0]

	if strings.Contains(username, ":") {
		return fmt.Errorf("username cannot contain ':'")
	}

	// Validate cost
	if htpasswdCost < bcrypt.MinCost || htpasswdCost > bcrypt.MaxCost {
		return fmt.Errorf("cost must be between %d and %d", bcrypt.MinCost, bcrypt.MaxCost)
	}

	// Read password
	password, err := readPassword()
	if err != nil {
		return err
	}

	// Generate bcrypt hash
	hash, err := bcrypt.GenerateFromPassword([]byte(password), htpasswdCost)
	if err != nil {
		return fmt.Errorf("failed to generate hash: %w", err)
	}

	// Output in htpasswd format
	fmt.Printf("%s:%s\n", username, string(hash))

	return nil
}

func readPassword() (string, error) {
	if term.IsTerminal(int(syscall.Stdin)) {
		fmt.Fprint(os.Stderr, "Password: ")
		password, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Fprintln(os.Stderr)

		if err != nil {
			return "", fmt.Errorf("failed to read password: %w", err)
		}

		fmt.Fprint(os.Stderr, "Confirm password: ")
		confirm, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Fprintln(os.Stderr)

		if err != nil {
			return "", fmt.Errorf("failed to read password confirmation: %w", err)
		}

		if string(password) != string(confirm) {
			return "", fmt.Errorf("passwords do not match")
		}

		return string(password), nil
	}

	reader := bufio.NewReader(os.Stdin)
	password, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read password from stdin: %w", err)
	}

	return strings.TrimSuffix(password, "\n"), nil
}
