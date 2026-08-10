package memory

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStoreInsertGet(t *testing.T) {
	s := newTestStore(t)
	m := &Memory{
		ID:       "abc",
		Type:     TypeFact,
		Content:  "DB is Postgres 16",
		Project:  "proj",
		Metadata: map[string]string{"k": "v"},
	}
	if err := s.Insert(context.Background(), m, []float64{1, 0}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, emb, err := s.Get(context.Background(), "abc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Content != m.Content || got.Project != "proj" || got.Metadata["k"] != "v" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if len(emb) != 2 || emb[0] != 1 {
		t.Fatalf("embedding roundtrip mismatch: %v", emb)
	}
}

func TestStoreGetMissing(t *testing.T) {
	s := newTestStore(t)
	if _, _, err := s.Get(context.Background(), "nope"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestStoreUpdateMergeMetadata(t *testing.T) {
	s := newTestStore(t)
	m := &Memory{ID: "u1", Type: TypeFact, Content: "a", Project: "p"}
	if err := s.Insert(context.Background(), m, []float64{1}); err != nil {
		t.Fatal(err)
	}
	imp := 0.9
	nc := "b"
	err := s.Update(context.Background(), "u1", UpdatePatch{
		Content:    &nc,
		Importance: &imp,
		Metadata:   map[string]string{"added": "1"},
		BumpAccess: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.Get(context.Background(), "u1")
	if got.Content != "b" || got.Importance != 0.9 || got.AccessCount != 1 {
		t.Fatalf("update mismatch: %+v", got)
	}
	if got.Metadata["added"] != "1" {
		t.Fatalf("metadata merge failed: %+v", got.Metadata)
	}
}

func TestStoreDelete(t *testing.T) {
	s := newTestStore(t)
	if err := s.Insert(context.Background(), &Memory{ID: "d1", Type: TypeFact, Content: "x"}, []float64{1}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(context.Background(), "d1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(context.Background(), "d1"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound on double delete, got %v", err)
	}
}

func TestStoreAllAndCount(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 3; i++ {
		if err := s.Insert(context.Background(),
			&Memory{ID: NewID(), Type: TypeFact, Content: "c", Project: "p1"}, []float64{1, 2, 3}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Insert(context.Background(),
		&Memory{ID: NewID(), Type: TypeFact, Content: "c", Project: "p2"}, []float64{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	all, _, err := s.All(context.Background(), "")
	if err != nil || len(all) != 4 {
		t.Fatalf("All: %d, %v", len(all), err)
	}
	p1, _, _ := s.All(context.Background(), "p1")
	if len(p1) != 3 {
		t.Fatalf("project filter: got %d", len(p1))
	}
	n, _ := s.Count(context.Background(), "p2")
	if n != 1 {
		t.Fatalf("count p2: %d", n)
	}
}
