// account-cleanup resumes an already authorized Firestore account deletion.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/firestorestore"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("account-cleanup", flag.ContinueOnError)
	project := flags.String("project", "", "Firestore project ID (required)")
	user := flags.String("user", "", "existing pending-deletion user ID (required)")
	timeout := flags.Duration("timeout", 5*time.Minute, "maximum time for this retry")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*project) == "" || strings.TrimSpace(*user) == "" || strings.ContainsAny(*user, "/\\") || flags.NArg() != 0 || *timeout <= 0 {
		return fmt.Errorf("require explicit -project, single-document -user, and positive -timeout")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	backend, err := firestorestore.New(ctx, *project)
	if err != nil {
		return err
	}
	defer backend.Close()
	if err := backend.Auth().ResumeAccountDeletion(ctx, *user); err != nil {
		return fmt.Errorf("resume existing account deletion: %w", err)
	}
	fmt.Println("Account cleanup complete.")
	return nil
}
