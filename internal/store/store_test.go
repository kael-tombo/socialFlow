package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestKeyRotation(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	org, oldKey, err := st.CreateOrgWithKey(ctx, "Banyan")
	if err != nil {
		t.Fatal(err)
	}
	newKey, err := st.AddKey(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddKey(ctx, "org_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("adding a key to a missing org: %v", err)
	}
	if _, err := st.RevokeKey(ctx, "abc"); err == nil {
		t.Fatal("a short prefix must be refused")
	}
	if _, err := st.RevokeKey(ctx, HashKey(oldKey)[:12]); err != nil {
		t.Fatal(err)
	}
	if _, err := st.OrgByKey(ctx, oldKey); !errors.Is(err, ErrNotFound) {
		t.Fatal("the revoked key must stop working")
	}
	if got, err := st.OrgByKey(ctx, newKey); err != nil || got.ID != org.ID {
		t.Fatalf("the new key must keep working: %v", err)
	}
	if _, err := st.RevokeKey(ctx, HashKey(oldKey)[:12]); err == nil {
		t.Fatal("revoking twice must say so")
	}
	keys, _ := st.ListKeys(ctx)
	if len(keys) != 2 || !keys[0].Revoked || keys[1].Revoked {
		t.Fatalf("keys: %+v", keys)
	}
}
