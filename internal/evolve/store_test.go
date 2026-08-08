package evolve

import (
	"path/filepath"
	"testing"
)

func TestStoreSaveGetList(t *testing.T) {
	userDir := t.TempDir()
	project := filepath.Join(t.TempDir(), "proj")
	st := StoreFor(userDir, project)
	if st.Dir == "" {
		t.Fatal("store dir empty")
	}
	p := baseProposal(TierL0)
	if err := st.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok := st.Get(p.ID)
	if !ok {
		t.Fatal("Get miss")
	}
	if got.Title != p.Title || got.Tier != TierL0 {
		t.Fatalf("got %+v", got)
	}
	list := st.List()
	if len(list) != 1 || list[0].ID != p.ID {
		t.Fatalf("List = %+v", list)
	}
}

func TestStoreRejectsBadID(t *testing.T) {
	st := StoreFor(t.TempDir(), t.TempDir())
	p := baseProposal(TierL0)
	p.ID = "../evil"
	if err := st.Save(p); err == nil {
		t.Fatal("expected bad id error")
	}
}
