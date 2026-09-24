// account-cleanup resumes already authorized Firestore account deletions.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/accountcleanup"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/firestorestore"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type factory func(context.Context, string) (accountcleanup.Store, func(), error)

func run(args []string) error {
	return execute(context.Background(), args, os.Stdout, func(ctx context.Context, project string) (accountcleanup.Store, func(), error) {
		backend, err := firestorestore.New(ctx, project)
		if err != nil {
			return nil, nil, err
		}
		return backend.Auth(), func() { _ = backend.Close() }, nil
	})
}

func execute(parent context.Context, args []string, output io.Writer, connect factory) error {
	flags := flag.NewFlagSet("account-cleanup", flag.ContinueOnError)
	// flag errors can reflect user IDs, cursor contents and file paths.
	flags.SetOutput(io.Discard)
	project := flags.String("project", "", "explicit Firestore project ID")
	options := accountcleanup.Options{}
	flags.StringVar(&options.User, "user", "", "one existing pending-deletion user ID")
	flags.BoolVar(&options.Pending, "pending", false, "process a bounded page of already pending deletions")
	flags.BoolVar(&options.DryRun, "dry-run", false, "list pending count without resuming deletion")
	flags.IntVar(&options.Limit, "limit", 25, "pending page size, 1-100")
	flags.DurationVar(&options.Timeout, "timeout", 5*time.Minute, "total deadline, maximum 15m")
	flags.DurationVar(&options.PerAccountTimeout, "account-timeout", 5*time.Minute, "per-account deadline, maximum 5m")
	cursorIn := flags.String("cursor-in", "", "read a protected cursor file from the same project")
	cursorOut := flags.String("cursor-out", "", "create a new protected next-cursor file; never overwrite")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(output)
			flags.PrintDefaults()
			return nil
		}
		return accountcleanup.ErrInvalid
	}
	if strings.TrimSpace(*project) == "" || strings.TrimSpace(*project) != *project || strings.ContainsAny(*project, "/\\\r\n") || flags.NArg() != 0 {
		return accountcleanup.ErrInvalid
	}
	invalidMode := false
	flags.Visit(func(f *flag.Flag) {
		if !options.Pending && (f.Name == "limit" || f.Name == "cursor-in" || f.Name == "cursor-out" || f.Name == "dry-run") {
			invalidMode = true
		}
	})
	if invalidMode {
		return accountcleanup.ErrInvalid
	}
	if *cursorIn != "" {
		file, err := os.Open(*cursorIn)
		if err != nil {
			return fmt.Errorf("cannot read cleanup cursor")
		}
		data, err := io.ReadAll(io.LimitReader(file, 2049))
		_ = file.Close()
		if err != nil || len(data) > 2048 {
			return accountcleanup.ErrInvalid
		}
		options.Cursor = string(data)
	}
	if err := options.Validate(); err != nil {
		return err
	}
	var checkpoint *os.File
	if *cursorOut != "" {
		var err error
		checkpoint, err = os.OpenFile(*cursorOut, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fmt.Errorf("cannot create new cleanup cursor file")
		}
		defer checkpoint.Close()
	}
	ctx, cancel := context.WithTimeout(parent, options.Timeout)
	defer cancel()
	store, closeStore, err := connect(ctx, *project)
	if err != nil {
		return fmt.Errorf("cannot initialize cleanup backend")
	}
	defer closeStore()
	result, runErr := accountcleanup.Run(ctx, store, options)
	if checkpoint != nil {
		if _, err := checkpoint.WriteString(result.NextCursor); err != nil {
			return fmt.Errorf("cannot save cleanup cursor; pending accounts may be retried from the beginning")
		}
		if err := checkpoint.Sync(); err != nil {
			return fmt.Errorf("cannot flush cleanup cursor; pending accounts may be retried from the beginning")
		}
	}
	if err := json.NewEncoder(output).Encode(result); err != nil {
		return fmt.Errorf("cannot write cleanup counts")
	}
	return runErr
}
