package clienttest

import (
	"context"
	"reflect"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestLogicalDatabaseFakePreservesIndependentLists(t *testing.T) {
	f := New()
	ctx := context.Background()
	list := []client.LogicalDatabase{{Name: "source_first", Schemas: []client.DatabaseSchema{{Name: "keep"}}}}
	roles := []client.DeclaredRole{{Name: "reporter"}}
	ext := []client.Extension{{Name: "pg_trgm"}}
	db, _, err := f.Databases().Create(ctx, client.CreateDatabaseRequest{Name: "restored", Postgres: &client.DatabasePostgres{Databases: &list, Roles: &roles, Extensions: &ext}})
	if err != nil {
		t.Fatal(err)
	}
	list[0].Schemas[0].Name = "mutated"
	if (*db.Postgres.Databases)[0].Schemas[0].Name != "keep" {
		t.Fatal("create aliases schemas")
	}
	next := []client.LogicalDatabase{{Name: "source_first", Schemas: []client.DatabaseSchema{{Name: "keep"}}}, {Name: "analytics", Schemas: []client.DatabaseSchema{}}}
	db, _, err = f.Databases().Update(ctx, "restored", "etag", client.PatchDatabaseRequest{Postgres: &client.DatabasePostgres{Databases: &next}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*db.Postgres.Roles, roles) || !reflect.DeepEqual(*db.Postgres.Extensions, ext) {
		t.Fatal("logical patch lost other lists")
	}
	next[0].Schemas[0].Name = "mutated"
	if (*db.Postgres.Databases)[0].Schemas[0].Name != "keep" {
		t.Fatal("patch aliases schemas")
	}
	emptyRoles := []client.DeclaredRole{}
	emptyExt := []client.Extension{}
	_, _, err = f.Databases().Update(ctx, "restored", "etag", client.PatchDatabaseRequest{Postgres: &client.DatabasePostgres{Roles: &emptyRoles, Extensions: &emptyExt}})
	if err != nil || len(*db.Postgres.Databases) != 2 {
		t.Fatalf("role/extension patch lost logical list: %v", err)
	}
	b, err := f.CreateDatabaseBinding(ctx, "restored", client.CreateDatabaseBindingRequest{Name: "first", Role: "owner"})
	if err != nil || b.LogicalDatabase != "source_first" {
		t.Fatalf("first binding=%+v err=%v", b, err)
	}
	b, err = f.CreateDatabaseBinding(ctx, "restored", client.CreateDatabaseBindingRequest{Name: "extra", Role: "readonly", LogicalDatabase: "analytics"})
	if err != nil || b.LogicalDatabase != "analytics" {
		t.Fatalf("extra binding=%+v err=%v", b, err)
	}
}
