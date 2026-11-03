package database

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

// These bounds mirror the published PostgreSQL policy. The API also enforces
// any smaller platform limits; the CLI does not read the services configuration.
const maxLogicalDatabases = 10
const maxDatabaseSchemas = 20

var logicalNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
var reservedDatabaseNames = []string{"all", "sameuser", "samerole", "samegroup", "replication", "postgres", "template0", "template1"}
var reservedLogicalPrefixes = []string{"pg_", "cnpg_", "mdba_", "mdbb_", "mdbl_"}

func validateLogicalName(name string) error {
	if len(name) > 53 || !logicalNamePattern.MatchString(name) {
		return cli.MisuseError(fmt.Sprintf("logical database name %q must start with a lowercase letter and contain only lowercase letters, numbers or _, at most 53 characters", name))
	}
	if slices.Contains(reservedDatabaseNames, name) {
		return cli.MisuseError(fmt.Sprintf("database name %q is reserved for PostgreSQL", name))
	}
	return nil
}

func validateAddedLogicalName(name string) error {
	if err := validateLogicalName(name); err != nil {
		return err
	}
	reserved := name == "app"
	for _, prefix := range reservedLogicalPrefixes {
		reserved = reserved || strings.HasPrefix(name+"_", prefix)
	}
	if reserved {
		return cli.MisuseError(fmt.Sprintf("%q cannot be used as an added database name: names starting with pg_, cnpg_, mdba_, mdbb_ or mdbl_, those words on their own, and app are reserved.", name))
	}
	return nil
}

func logicalRequested(names, schemas []string) bool { return len(names)+len(schemas) > 0 }

// appendLogical always copies the effective list from the latest read. It never
// derives a restored database's identity from its instance name.
func appendLogical(current []client.LogicalDatabase, names, schemas []string) ([]client.LogicalDatabase, []string, error) {
	out := make([]client.LogicalDatabase, len(current))
	for i, db := range current {
		out[i] = db
		out[i].Schemas = slices.Clone(db.Schemas)
	}
	var notes []string
	for _, name := range names {
		if err := validateLogicalName(name); err != nil {
			return nil, nil, err
		}
		if slices.ContainsFunc(out, func(db client.LogicalDatabase) bool { return db.Name == name }) {
			notes = append(notes, fmt.Sprintf("Logical database %q already exists; keeping it.", name))
			continue
		}
		if err := validateAddedLogicalName(name); err != nil {
			return nil, nil, err
		}
		out = append(out, client.LogicalDatabase{Name: name, Schemas: []client.DatabaseSchema{}})
	}
	if len(out) > maxLogicalDatabases {
		return nil, nil, cli.MisuseError("a database can hold at most 10 logical databases, including the first")
	}
	for _, value := range schemas {
		target, schema, qualified := strings.Cut(value, ".")
		if !qualified {
			schema, target = value, out[0].Name
		}
		if err := validateLogicalName(target); err != nil {
			return nil, nil, err
		}
		if len(schema) > 63 || !logicalNamePattern.MatchString(schema) || strings.HasPrefix(schema, "pg_") || schema == "public" || schema == "information_schema" {
			return nil, nil, cli.MisuseError(fmt.Sprintf("schema name %q is invalid or reserved", schema))
		}
		index := slices.IndexFunc(out, func(db client.LogicalDatabase) bool { return db.Name == target })
		if index < 0 {
			available := make([]string, len(out))
			for i, db := range out {
				available[i] = db.Name
			}
			return nil, nil, cli.MisuseError(fmt.Sprintf("logical database %q is not in this database; available: %s", target, strings.Join(available, ", ")))
		}
		if slices.ContainsFunc(out[index].Schemas, func(s client.DatabaseSchema) bool { return s.Name == schema }) {
			notes = append(notes, fmt.Sprintf("Schema %q already exists; keeping it.", target+"."+schema))
			continue
		}
		out[index].Schemas = append(out[index].Schemas, client.DatabaseSchema{Name: schema})
		if len(out[index].Schemas) > maxDatabaseSchemas {
			return nil, nil, cli.MisuseError(fmt.Sprintf("database %q may have at most 20 schemas", target))
		}
	}
	return out, notes, nil
}

func logicalNotAvailable() error {
	return cli.New(cli.ExitUnavailable, "the platform does not report this database's logical databases yet").WithHint("the platform must offer logical databases before they can be changed")
}

// logicalFailed ignores status from an older spec generation.
func logicalFailed(db *client.Database) bool {
	if db.Status == nil || db.Status.ObservedGeneration < db.Generation {
		return false
	}
	return slices.ContainsFunc(db.Status.Databases, func(st client.LogicalDatabaseStatus) bool { return st.Phase == client.PhaseFailed })
}

func logicalSettled(db *client.Database) bool {
	if db.Postgres == nil || db.Postgres.Databases == nil {
		return false
	}
	if db.Status == nil {
		return false
	}
	for _, entry := range *db.Postgres.Databases {
		if !slices.ContainsFunc(db.Status.Databases, func(st client.LogicalDatabaseStatus) bool {
			return st.Name == entry.Name && st.Phase == client.PhaseReady
		}) {
			return false
		}
	}
	return true
}

// Logical edits need a stricter wait than the explicit "wait --for running"
// command, which continues to mean the instance's Running phase only.
func waitForLogicalConverged(ctx context.Context, streams *cli.IOStreams, api client.Interface, name string, generation int64, timeout time.Duration) (*client.Database, error) {
	logicalService := service
	logicalService.Phase = func(db *client.Database) string {
		if logicalFailed(db) {
			return client.PhaseFailed
		}
		return service.Phase(db)
	}
	logicalService.Converge.Settled = func(db *client.Database) bool { return db != nil && db.Status.Ready() && logicalSettled(db) }
	return logicalService.WaitForConverged(ctx, streams, api, name, generation, timeout)
}
