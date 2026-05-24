// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package blobstore_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/mattdurham/lth/internal/blobstore"
)

func newTestStore(t *testing.T) *blobstore.LocalStore {
	t.Helper()
	s, err := blobstore.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	return s
}

func TestLocalStore_PutGet(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	data := []byte("hello world")
	if err := s.Put(ctx, "testkey", bytes.NewReader(data)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rc, err := s.Get(ctx, "testkey")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("got %q want %q", got, data)
	}
}

func TestLocalStore_GetMissing(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, err := s.Get(ctx, "nonexistent")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected fs.ErrNotExist, got %v", err)
	}
}

func TestLocalStore_Exists_True(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.Put(ctx, "k", strings.NewReader("v")); err != nil {
		t.Fatal(err)
	}
	ok, err := s.Exists(ctx, "k")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Error("expected true")
	}
}

func TestLocalStore_Exists_False(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	ok, err := s.Exists(ctx, "missing")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if ok {
		t.Error("expected false")
	}
}

func TestLocalStore_Delete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.Put(ctx, "del", strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "del"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := s.Get(ctx, "del")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected fs.ErrNotExist after delete, got %v", err)
	}
}

func TestLocalStore_List_Prefix(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	for _, k := range []string{"prefix/a", "prefix/b", "prefix/c", "other/d"} {
		if err := s.Put(ctx, k, strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
	}
	objs, err := s.List(ctx, "prefix/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(objs) != 3 {
		t.Errorf("got %d objects want 3", len(objs))
	}
}

func TestLocalStore_List_Empty(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	objs, err := s.List(ctx, "nope/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(objs) != 0 {
		t.Errorf("expected empty slice, got %d", len(objs))
	}
	if objs == nil {
		t.Error("expected non-nil empty slice")
	}
}

func TestLocalStore_Key_Slashes(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.Put(ctx, "a/b/c", strings.NewReader("deep")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rc, err := s.Get(ctx, "a/b/c")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "deep" {
		t.Errorf("got %q", data)
	}
}

func TestLocalStore_PathTraversal(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	err := s.Put(ctx, "../escape", strings.NewReader("bad"))
	if err == nil {
		t.Error("expected error for path traversal key")
	}
}

func TestLocalStore_Delete_Missing(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	// Delete of non-existent key should not error.
	if err := s.Delete(ctx, "ghost"); err != nil {
		t.Errorf("Delete missing key: %v", err)
	}
}
