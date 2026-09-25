// Command mig-verify reproduces the bootstrap migration path
// (internal/bootstrap/app.go runPostSchemaMigrations) against an arbitrary
// database so disk-discovered migrations can be exercised with up/down.
//
// Temporary verification harness for the multi-LLM-provider BE-1 gate.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"go.uber.org/zap"

	"itsm-backend/config"
	"itsm-backend/database"
	"itsm-backend/migration"
)

func main() {
	status := flag.Bool("status", false, "print applied/pending then exit")
	ro := flag.Bool("ro", false, "strictly read-only preflight: status only, no writes (safe for prod)")
	up := flag.Bool("up", false, "run post-schema + filesystem migrations")
	down := flag.Int("down", 0, "rollback the last N applied migrations")
	only := flag.String("only", "", "comma-separated pending versions to apply (default: all)")
	entBaseline := flag.Bool("entbaseline", false, "run ent Schema.Create first (fresh-install rehearsal)")
	flag.Parse()

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	sugar := logger.Sugar()
	fmt.Printf("TARGET host=%s port=%d db=%s user=%s\n",
		cfg.Database.Host, cfg.Database.Port, cfg.Database.DBName, cfg.Database.User)

	db, err := database.InitDB(&cfg.Database)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// -ro：严格只读预检（不建账本、不跑注册迁移、不补账/收养），可安全指向 prod。
	// 走与 bootstrap 相同的 merged 流 + GetPendingMigrations 校验，因此能复现
	// 「checksum mismatch 卡住全部 pending」这类上线阻塞。
	if *ro {
		migrator := migration.NewMigrator(db, sugar)
		fsMigs, err := migration.FilesystemMigrations("")
		if err != nil {
			log.Fatalf("discover migrations: %v", err)
		}
		merged := migration.MergeWithRegistered(fsMigs)
		fmt.Printf("RO MODE (read-only): discovered=%d merged=%d\n", len(fsMigs), len(merged))
		applied, pending, err := migrator.Status(ctx, merged)
		if err != nil {
			fmt.Printf("STATUS ERROR (read-only): %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("applied=%d pending=%d\n", len(applied), len(pending))
		for _, m := range pending {
			fmt.Printf("  PENDING %s | %s\n", m.Version, m.Description)
		}
		return
	}

	if *entBaseline {
		client, err := database.InitDatabase(&cfg.Database)
		if err != nil {
			log.Fatalf("ent client: %v", err)
		}
		if err := client.Schema.Create(ctx); err != nil {
			log.Fatalf("ent schema create: %v", err)
		}
		if err := client.Close(); err != nil {
			log.Fatalf("ent client close: %v", err)
		}
		fmt.Println("ENT BASELINE created (Schema.Create)")
	}

	migrator := migration.NewMigrator(db, sugar)
	if err := migrator.EnsureMigrationsTable(ctx); err != nil {
		log.Fatalf("ensure ledger: %v", err)
	}

	// Step 1: registered post-schema migrations (same as bootstrap).
	if _, err := migrator.RunMigrations(ctx, migration.PostSchemaMigrations()); err != nil {
		log.Fatalf("post-schema migrations: %v", err)
	}

	// Step 2: disk discovery + ledger reconciliation + merged run (same as bootstrap).
	fsMigs, err := migration.FilesystemMigrations("")
	if err != nil {
		log.Fatalf("discover migrations: %v", err)
	}
	if err := migration.RecordLegacyMigrationsApplied(ctx, db, sugar); err != nil {
		log.Fatalf("record legacy: %v", err)
	}
	adopted, err := migration.AdoptUnrecordedFilesystemMigrations(ctx, db, fsMigs, sugar)
	if err != nil {
		log.Fatalf("adopt: %v", err)
	}
	merged := migration.MergeWithRegistered(fsMigs)
	fmt.Printf("discovered=%d merged=%d pre-discovery-adopted=%d\n", len(fsMigs), len(merged), adopted)

	applied, pending, err := migrator.Status(ctx, merged)
	if err != nil {
		if *status {
			log.Fatalf("status: %v", err)
		}
		// 继续走 -up/-down 路径，让真实的执行点返回错误（证据更贴近 bootstrap）。
		fmt.Printf("STATUS ERROR (continuing): %v\n", err)
		if applied, err = migrator.GetAppliedMigrations(ctx); err != nil {
			log.Fatalf("applied migrations: %v", err)
		}
	} else {
		fmt.Printf("applied=%d pending=%d\n", len(applied), len(pending))
		for _, m := range pending {
			fmt.Printf("  PENDING %s | %s\n", m.Version, m.Description)
		}
		n := len(applied)
		if n > 6 {
			n = 6
		}
		fmt.Println("  -- last applied --")
		for _, m := range applied[len(applied)-n:] {
			fmt.Printf("  APPLIED %s | %s\n", m.Version, m.Description)
		}
	}

	if *status {
		return
	}

	if *up {
		runList := merged
		if *only != "" {
			wanted := map[string]bool{}
			for _, v := range strings.Split(*only, ",") {
				wanted[strings.TrimSpace(v)] = true
			}
			runList = nil
			for _, m := range pending {
				if wanted[m.Version] {
					runList = append(runList, m)
					delete(wanted, m.Version)
				}
			}
			if len(wanted) > 0 {
				log.Fatalf("requested versions not pending: %v", wanted)
			}
			fmt.Printf("only-mode: %d migration(s) selected\n", len(runList))
		}
		count, err := migrator.RunMigrations(ctx, runList)
		if err != nil {
			log.Fatalf("apply migrations: %v", err)
		}
		fmt.Printf("APPLIED %d migration(s)\n", count)
		return
	}

	if *down > 0 {
		// 回滚口径：按「实际应用时间倒序」而非版本号字典序
		// （add_missing_indexes* 等非日期别名会排在日期版本之后，见 cmd/migrate 的同款缺陷）。
		sort.SliceStable(applied, func(i, j int) bool {
			ti, tj := applied[i].AppliedAt, applied[j].AppliedAt
			if ti == nil || tj == nil {
				return applied[i].Version > applied[j].Version
			}
			return ti.After(*tj)
		})
		done := 0
		for i := 0; i < len(applied) && done < *down; i++ {
			m := applied[i]
			if m.RollbackSQL == "" {
				log.Fatalf("migration %s has no rollback SQL", m.Version)
			}
			if err := migrator.RollbackMigration(ctx, m); err != nil {
				log.Fatalf("rollback %s: %v", m.Version, err)
			}
			fmt.Printf("ROLLED BACK %s\n", m.Version)
			done++
		}
		if done < *down {
			log.Fatalf("only rolled back %d of %d requested", done, *down)
		}
		return
	}
	fmt.Println("no action selected (use -status/-up/-down N)")
}
