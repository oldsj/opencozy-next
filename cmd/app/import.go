package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"

	"github.com/oldsj/opencozy-next/internal/config"
	"github.com/oldsj/opencozy-next/internal/db"
	"github.com/oldsj/opencozy-next/internal/importer"
	"github.com/oldsj/opencozy-next/internal/importer/oldsecret"
)

// runImport is `app import`: copy the old app (e717170) from a restored backup into this new,
// empty installation, in one transaction (design §12). It migrates first, as the schema owner.
func runImport(ctx context.Context, cfg config.Config, log *slog.Logger, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	source := fs.String("source-url", "", "the old database (a restored backup, never the live one); put its password in PGPASSWORD or a passfile, not the URL")
	dryRun := fs.Bool("dry-run", false, "import, print the reconciliation, and roll back")
	oldSecretFile := fs.String("old-auth-secret-file", "", "a file holding the old app's BETTER_AUTH_SECRET, to carry Google, iCloud and Home Assistant secrets; read once, never stored")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *source == "" {
		return errors.New("import: --source-url is required")
	}
	var old *oldsecret.Secret
	if *oldSecretFile != "" {
		var err error
		if oldsecret.Loose(*oldSecretFile) {
			log.WarnContext(ctx, "the old auth secret file is readable by group or others; chmod 600 it", "file", *oldSecretFile)
		}
		if old, err = oldsecret.Load(*oldSecretFile); err != nil {
			return err
		}
		defer old.Forget()
	}
	if err := migrate(ctx, cfg, log); err != nil {
		return err
	}
	owner, err := db.Open(ctx, cfg.MigrationURL())
	if err != nil {
		return err
	}
	defer owner.Close()
	return importer.Run(ctx, importer.Options{SourceURL: *source, Target: owner, DryRun: *dryRun, Out: stdout, Keys: cfg.SecretKeys, OldSecret: old})
}
