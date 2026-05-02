package main

import (
	"context"
	"flag"
	"fmt"

	storagemigrate "github.com/lost-coder/panvex/internal/controlplane/storage/migrate"
)

func runMigrateStorage(args []string) error {
	flags := flag.NewFlagSet("migrate-storage", flag.ContinueOnError)
	sourceDriver := flags.String("from-driver", "sqlite", "Source storage backend driver")
	sourceDSN := flags.String("from-dsn", "", "Source storage backend DSN")
	targetDriver := flags.String("to-driver", "postgres", "Target storage backend driver")
	targetDSN := flags.String("to-dsn", "", "Target storage backend DSN")
	if err := flags.Parse(args); err != nil {
		return err
	}

	summary, err := storagemigrate.Run(context.Background(), storagemigrate.Options{
		SourceDriver: *sourceDriver,
		SourceDSN:    *sourceDSN,
		TargetDriver: *targetDriver,
		TargetDSN:    *targetDSN,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Migration completed.\n")
	fmt.Printf("Users: %d\n", summary.Users)
	fmt.Printf("Fleet groups: %d\n", summary.FleetGroups)
	fmt.Printf("Agents: %d\n", summary.Agents)
	fmt.Printf("Instances: %d\n", summary.Instances)
	fmt.Printf("Jobs: %d\n", summary.Jobs)
	fmt.Printf("Job targets: %d\n", summary.JobTargets)
	fmt.Printf("Audit events: %d\n", summary.AuditEvents)
	fmt.Printf("Metric snapshots: %d\n", summary.MetricSnapshots)
	fmt.Printf("Enrollment tokens: %d\n", summary.EnrollmentTokens)
	return nil
}
