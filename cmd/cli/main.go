package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	dbembed "github.com/benpsk/go-starter/db"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/postgres"
)

const (
	defaultMigrationsDir = "db/migrations"
	defaultSeedersDir    = "db/seeders"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	if len(os.Args) < 2 {
		log.Fatalf("usage: %s [migrate|seed|fresh|dump] [options]", os.Args[0])
	}

	switch os.Args[1] {
	case "migrate":
		runMigrate(os.Args[2:])
	case "seed":
		runSeed(os.Args[2:])
	case "fresh":
		runFresh(os.Args[2:])
	case "dump":
		runDump(os.Args[2:])
	default:
		log.Fatalf("usage: %s [migrate|seed|fresh|dump] [options]", os.Args[0])
	}
}

func runMigrate(args []string) {
	flags := flag.NewFlagSet("migrate", flag.ExitOnError)
	migrationsDir := flags.String("path", defaultMigrationsDir, "directory containing .sql migrations (overrides embedded bundle)")
	_ = flags.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	pool, err := postgres.Connect(ctx, cfg.Database)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	useEmbedded, err := shouldUseEmbedded(*migrationsDir, defaultMigrationsDir)
	if err != nil {
		log.Fatalf("migrate: %v", err)
	}
	if err := postgres.EnsureTable(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	var applied []string
	if useEmbedded {
		migrationsFS, err := fs.Sub(dbembed.Migrations, "migrations")
		if err != nil {
			log.Fatalf("migrate: %v", err)
		}
		applied, err = postgres.ApplyFS(ctx, pool, migrationsFS)
		if err != nil {
			log.Fatalf("migrate: %v", err)
		}
	} else {
		applied, err = postgres.Apply(ctx, pool, *migrationsDir)
		if err != nil {
			log.Fatalf("migrate: %v", err)
		}
	}

	if len(applied) == 0 {
		log.Println("migrate: no migrations applied")
		return
	}
	for _, name := range applied {
		log.Printf("migrate: applied %s", name)
	}
}

func runSeed(args []string) {
	flags := flag.NewFlagSet("seed", flag.ExitOnError)
	seedersDir := flags.String("path", defaultSeedersDir, "directory containing .sql seeders (overrides embedded bundle)")
	_ = flags.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	pool, err := postgres.Connect(ctx, cfg.Database)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	useEmbedded, err := shouldUseEmbedded(*seedersDir, defaultSeedersDir)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	if err := postgres.EnsureSeedTable(ctx, pool); err != nil {
		log.Fatalf("seed: %v", err)
	}

	var applied []string
	if useEmbedded {
		seedersFS, err := fs.Sub(dbembed.Seeders, "seeders")
		if err != nil {
			log.Fatalf("seed: %v", err)
		}
		applied, err = postgres.SeedFS(ctx, pool, seedersFS)
		if err != nil {
			log.Fatalf("seed: %v", err)
		}
	} else {
		applied, err = postgres.Seed(ctx, pool, *seedersDir)
		if err != nil {
			log.Fatalf("seed: %v", err)
		}
	}

	if len(applied) == 0 {
		log.Println("seed: no seeders applied")
		return
	}
	for _, name := range applied {
		log.Printf("seed: applied %s", name)
	}
}

func runFresh(args []string) {
	flags := flag.NewFlagSet("fresh", flag.ExitOnError)
	migrationsDir := flags.String("path", defaultMigrationsDir, "directory containing .sql migrations (overrides embedded bundle)")
	seed := flags.Bool("seed", false, "apply seed files after migrations")
	seedersDir := flags.String("seed-path", defaultSeedersDir, "directory containing .sql seeders (overrides embedded bundle)")
	_ = flags.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.AppEnv != "development" {
		log.Fatalf("fresh: APP_ENV must be development (got %q)", cfg.AppEnv)
	}

	pool, err := postgres.Connect(ctx, cfg.Database)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	if err := postgres.ResetSchema(ctx, pool); err != nil {
		log.Fatalf("fresh: %v", err)
	}

	useEmbeddedMigrations, err := shouldUseEmbedded(*migrationsDir, defaultMigrationsDir)
	if err != nil {
		log.Fatalf("fresh: %v", err)
	}
	if err := postgres.EnsureTable(ctx, pool); err != nil {
		log.Fatalf("fresh: %v", err)
	}

	var applied []string
	if useEmbeddedMigrations {
		migrationsFS, err := fs.Sub(dbembed.Migrations, "migrations")
		if err != nil {
			log.Fatalf("fresh: %v", err)
		}
		applied, err = postgres.ApplyFS(ctx, pool, migrationsFS)
		if err != nil {
			log.Fatalf("fresh: %v", err)
		}
	} else {
		applied, err = postgres.Apply(ctx, pool, *migrationsDir)
		if err != nil {
			log.Fatalf("fresh: %v", err)
		}
	}
	for _, name := range applied {
		log.Printf("fresh: applied %s", name)
	}

	if !*seed {
		return
	}

	useEmbeddedSeeders, err := shouldUseEmbedded(*seedersDir, defaultSeedersDir)
	if err != nil {
		log.Fatalf("fresh: %v", err)
	}
	if err := postgres.EnsureSeedTable(ctx, pool); err != nil {
		log.Fatalf("fresh: %v", err)
	}

	var seeded []string
	if useEmbeddedSeeders {
		seedersFS, err := fs.Sub(dbembed.Seeders, "seeders")
		if err != nil {
			log.Fatalf("fresh: %v", err)
		}
		seeded, err = postgres.SeedFS(ctx, pool, seedersFS)
		if err != nil {
			log.Fatalf("fresh: %v", err)
		}
	} else {
		seeded, err = postgres.Seed(ctx, pool, *seedersDir)
		if err != nil {
			log.Fatalf("fresh: %v", err)
		}
	}
	for _, name := range seeded {
		log.Printf("fresh: applied seed %s", name)
	}
}

func runDump(args []string) {
	flags := flag.NewFlagSet("dump", flag.ExitOnError)
	out := flags.String("out", defaultDumpPath(), "output file path")
	schemaOnly := flags.Bool("schema-only", false, "dump schema only")
	dataOnly := flags.Bool("data-only", false, "dump data only")
	binary := flags.String("pg-dump-bin", "pg_dump", "pg_dump binary path")
	_ = flags.Parse(args)

	if *schemaOnly && *dataOnly {
		log.Fatal("dump: choose only one of -schema-only or -data-only")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		log.Fatalf("dump: mkdir output dir: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	argsOut := []string{
		"--dbname", cfg.Database.URL,
		"--format=plain",
		"--no-owner",
		"--no-privileges",
		"--file", *out,
	}
	if *schemaOnly {
		argsOut = append(argsOut, "--schema-only")
	}
	if *dataOnly {
		argsOut = append(argsOut, "--data-only")
	}

	cmd := exec.CommandContext(ctx, *binary, argsOut...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("dump: running %s -> %s", *binary, *out)
	if err := cmd.Run(); err != nil {
		log.Fatalf("dump: %v", err)
	}
	fmt.Printf("dump written: %s\n", *out)
}

func defaultDumpPath() string {
	return filepath.Join("tmp", "dump-"+time.Now().Format("20060102-150405")+".sql")
}

func shouldUseEmbedded(path, defaultPath string) (bool, error) {
	if path == "" {
		return true, nil
	}

	info, err := os.Stat(path)
	switch {
	case err == nil:
		if !info.IsDir() {
			return false, fmt.Errorf("path %q is not a directory", path)
		}
		return false, nil
	case errors.Is(err, os.ErrNotExist):
		if path == defaultPath {
			return true, nil
		}
		return false, fmt.Errorf("path %q not found", path)
	default:
		return false, fmt.Errorf("stat path %q: %w", path, err)
	}
}
