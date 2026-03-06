package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	db, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCreateAndGet(t *testing.T) {
	db := newTestDB(t)

	rec := &ConnectionRecord{
		Name:     "gh-main",
		Platform: "github",
		Token:    "ghp_secret",
		BaseURL:  "https://api.github.com",
	}

	if err := db.Create(rec); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if rec.ID == 0 {
		t.Error("expected non-zero ID after create")
	}

	got, err := db.GetByName("gh-main")
	if err != nil {
		t.Fatalf("GetByName failed: %v", err)
	}
	if got.Name != "gh-main" {
		t.Errorf("expected gh-main, got %s", got.Name)
	}
	if got.Token != "ghp_secret" {
		t.Errorf("expected token preserved, got %s", got.Token)
	}
	if got.Platform != "github" {
		t.Errorf("expected github, got %s", got.Platform)
	}
}

func TestCreateDuplicate(t *testing.T) {
	db := newTestDB(t)
	db.Create(&ConnectionRecord{Name: "gh-main", Platform: "github"})
	err := db.Create(&ConnectionRecord{Name: "gh-main", Platform: "github"})
	if err == nil {
		t.Error("expected error for duplicate name")
	}
}

func TestList(t *testing.T) {
	db := newTestDB(t)
	db.Create(&ConnectionRecord{Name: "gh-1", Platform: "github"})
	db.Create(&ConnectionRecord{Name: "gl-1", Platform: "gitlab"})
	db.Create(&ConnectionRecord{Name: "bb-1", Platform: "bitbucket"})

	records, err := db.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(records) != 3 {
		t.Errorf("expected 3 records, got %d", len(records))
	}
}

func TestListByPlatform(t *testing.T) {
	db := newTestDB(t)
	db.Create(&ConnectionRecord{Name: "gh-1", Platform: "github"})
	db.Create(&ConnectionRecord{Name: "gh-2", Platform: "github"})
	db.Create(&ConnectionRecord{Name: "gl-1", Platform: "gitlab"})

	ghRecords, err := db.ListByPlatform("github")
	if err != nil {
		t.Fatalf("ListByPlatform failed: %v", err)
	}
	if len(ghRecords) != 2 {
		t.Errorf("expected 2 github records, got %d", len(ghRecords))
	}

	glRecords, _ := db.ListByPlatform("gitlab")
	if len(glRecords) != 1 {
		t.Errorf("expected 1 gitlab record, got %d", len(glRecords))
	}

	bbRecords, _ := db.ListByPlatform("bitbucket")
	if len(bbRecords) != 0 {
		t.Errorf("expected 0 bitbucket records, got %d", len(bbRecords))
	}
}

func TestUpdate(t *testing.T) {
	db := newTestDB(t)
	db.Create(&ConnectionRecord{Name: "gh-main", Platform: "github", Token: "old"})

	err := db.Update(&ConnectionRecord{Name: "gh-main", Platform: "github", Token: "new", BaseURL: "https://ghe.example.com"})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, _ := db.GetByName("gh-main")
	if got.Token != "new" {
		t.Errorf("expected new token, got %s", got.Token)
	}
	if got.BaseURL != "https://ghe.example.com" {
		t.Errorf("expected updated base URL, got %s", got.BaseURL)
	}
}

func TestUpdateNotFound(t *testing.T) {
	db := newTestDB(t)
	err := db.Update(&ConnectionRecord{Name: "nonexistent"})
	if err == nil {
		t.Error("expected error for updating nonexistent record")
	}
}

func TestDelete(t *testing.T) {
	db := newTestDB(t)
	db.Create(&ConnectionRecord{Name: "gh-main", Platform: "github"})

	if err := db.Delete("gh-main"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	count, _ := db.Count()
	if count != 0 {
		t.Errorf("expected 0 after delete, got %d", count)
	}
}

func TestDeleteNotFound(t *testing.T) {
	db := newTestDB(t)
	err := db.Delete("nonexistent")
	if err == nil {
		t.Error("expected error for deleting nonexistent record")
	}
}

func TestCount(t *testing.T) {
	db := newTestDB(t)
	count, _ := db.Count()
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}

	db.Create(&ConnectionRecord{Name: "a", Platform: "github"})
	db.Create(&ConnectionRecord{Name: "b", Platform: "gitlab"})
	count, _ = db.Count()
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestGetNotFound(t *testing.T) {
	db := newTestDB(t)
	_, err := db.GetByName("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent record")
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")

	db1, _ := New(dbPath)
	db1.Create(&ConnectionRecord{Name: "gh-main", Platform: "github", Token: "secret"})
	db1.Close()

	db2, _ := New(dbPath)
	defer db2.Close()

	got, err := db2.GetByName("gh-main")
	if err != nil {
		t.Fatalf("expected record to persist: %v", err)
	}
	if got.Token != "secret" {
		t.Errorf("expected secret token after reopen, got %s", got.Token)
	}
}

func TestDBFileCreation(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "new.db")

	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create DB: %v", err)
	}
	db.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("expected database file to be created")
	}
}
