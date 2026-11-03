package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"testing"
)

func TestLogicalDatabaseRoundTrip(t *testing.T) {
	const response = `{"name":"restored","postgres":{"databases":[{"name":"source_first","schemas":[{"name":"keep"}]},{"name":"analytics","schemas":[]}]},"status":{"databases":[{"name":"analytics","phase":"Failed","message":"schema refused"}]}}`
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, response) })
	db, _, err := c.Databases().Get(context.Background(), "restored")
	if err != nil {
		t.Fatal(err)
	}
	if db.Postgres == nil || db.Postgres.Databases == nil || len(*db.Postgres.Databases) != 2 || (*db.Postgres.Databases)[0].Schemas[0].Name != "keep" || db.Status.Databases[0].Message != "schema refused" {
		t.Fatalf("decoded %+v", db)
	}
	raw, err := json.Marshal(db)
	if err != nil {
		t.Fatal(err)
	}
	var round Database
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(db, &round) {
		t.Fatalf("roundtrip %+v", round)
	}
	var explicit DatabasePostgres
	if err := json.Unmarshal([]byte(`{"databases":[],"roles":[],"extensions":[]}`), &explicit); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"databases":[],"roles":[],"extensions":[]}` {
		t.Fatalf("empty lists = %s", raw)
	}
	var omitted DatabasePostgres
	raw, err = json.Marshal(omitted)
	if err != nil || string(raw) != "{}" {
		t.Fatalf("omitted = %s %v", raw, err)
	}
}

// This fixture is extracted unchanged from the independently reviewed MDB2-4c
// Swagger. Provenance and full-source hashes live in the card's evidence.
func TestLogicalClientMatchesSwagger(t *testing.T) {
	raw, err := os.ReadFile("testdata/database-logical-swagger.json")
	if err != nil {
		t.Fatal(err)
	}
	var definitions map[string]struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &definitions); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		value  any
		fields []string
	}{
		{"server.databasePostgres", DatabasePostgres{}, []string{"databases", "roles", "extensions"}},
		{"server.logicalDatabase", LogicalDatabase{}, []string{"name", "schemas"}},
		{"server.databaseSchema", DatabaseSchema{}, []string{"name"}},
		{"server.logicalDatabaseStatus", LogicalDatabaseStatus{}, []string{"name", "phase", "message"}},
		{"server.databaseStatus", DatabaseStatus{}, []string{"databases"}},
		{"server.createDatabaseBindingRequest", CreateDatabaseBindingRequest{}, []string{"logicalDatabase"}},
		{"server.databaseBindingResponse", DatabaseBinding{}, []string{"logicalDatabase"}},
	} {
		fields := map[string]bool{}
		typ := reflect.TypeOf(tt.value)
		for i := 0; i < typ.NumField(); i++ {
			tag := typ.Field(i).Tag.Get("json")
			for j, c := range tag {
				if c == ',' {
					tag = tag[:j]
					break
				}
			}
			fields[tag] = true
		}
		for _, field := range tt.fields {
			if !fields[field] || definitions[tt.name].Properties[field] == nil {
				t.Errorf("%s missing %s in client/Swagger", tt.name, field)
			}
		}
	}
	for _, value := range []any{Database{}, CreateDatabaseRequest{}, PatchDatabaseRequest{}} {
		typ := reflect.TypeOf(value)
		for i := 0; i < typ.NumField(); i++ {
			if typ.Field(i).Name == "Databases" {
				t.Errorf("%s has top-level databases", typ.Name())
			}
		}
	}
}
