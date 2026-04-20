package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestListMembers(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"items":[
		  {"email":"alice@acme.com","role":"admin"},
		  {"email":"bob@acme.com","role":"readonly"}
		]}`)
	})
	list, err := c.ListMembers(context.Background())
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(list) != 2 || list[0].Email != "alice@acme.com" {
		t.Fatalf("unexpected: %+v", list)
	}
}

func TestAddMember(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body AddMemberRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Email != "x@y.com" || body.Role != "admin" {
			t.Errorf("unexpected body: %+v", body)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintln(w, `{"email":"x@y.com","role":"admin"}`)
	})
	m, err := c.AddMember(context.Background(), AddMemberRequest{Email: "x@y.com", Role: "admin"})
	if err != nil || m.Email != "x@y.com" {
		t.Fatalf("AddMember: %v, %+v", err, m)
	}
}

func TestAddMemberConflict(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintln(w, `{"error":"member already exists"}`)
	})
	_, err := c.AddMember(context.Background(), AddMemberRequest{Email: "dup@x.com", Role: "admin"})
	if !IsConflict(err) {
		t.Fatalf("want IsConflict, got %v", err)
	}
}

func TestUpdateMember(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s", r.Method)
		}
		var body UpdateMemberRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Role != "readonly" {
			t.Errorf("role = %q", body.Role)
		}
		fmt.Fprintln(w, `{"email":"x@y.com","role":"readonly"}`)
	})
	m, err := c.UpdateMember(context.Background(), "x@y.com", UpdateMemberRequest{Role: "readonly"})
	if err != nil || m.Role != "readonly" {
		t.Fatalf("UpdateMember: %v, %+v", err, m)
	}
}

func TestRemoveMember(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.RemoveMember(context.Background(), "x@y.com"); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
}

func TestRemoveMemberNotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"error":"member not found"}`)
	})
	err := c.RemoveMember(context.Background(), "ghost@nowhere.com")
	if !IsNotFound(err) {
		t.Fatalf("want IsNotFound, got %v", err)
	}
}
