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

func TestFindingLifecycle(t *testing.T) {
	db := newTestDB(t)

	// Create a finding
	f := &FindingRecord{
		ConnectionName: "gh-main",
		RunID:          "42",
		Severity:       "high",
		Category:       "secrets",
		Title:          "Hardcoded secret",
		Description:    "AWS key found in logs",
		Remediation:    "Use secrets manager",
		Confidence:     0.95,
		Status:         "open",
	}
	if err := db.CreateFinding(f); err != nil {
		t.Fatalf("CreateFinding failed: %v", err)
	}
	if f.ID == 0 {
		t.Error("expected non-zero ID")
	}

	// List findings
	findings, err := db.ListFindings("")
	if err != nil {
		t.Fatalf("ListFindings failed: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Status != "open" {
		t.Errorf("expected open status, got %s", findings[0].Status)
	}

	// Update status to acknowledged
	if err := db.UpdateFindingStatus(f.ID, "acknowledged"); err != nil {
		t.Fatalf("UpdateFindingStatus failed: %v", err)
	}
	findings, _ = db.ListFindings("")
	if findings[0].Status != "acknowledged" {
		t.Errorf("expected acknowledged, got %s", findings[0].Status)
	}

	// Update to false_positive should set false_positive flag
	if err := db.UpdateFindingStatus(f.ID, "false_positive"); err != nil {
		t.Fatalf("UpdateFindingStatus failed: %v", err)
	}
	findings, _ = db.ListFindings("")
	if findings[0].Status != "false_positive" {
		t.Errorf("expected false_positive, got %s", findings[0].Status)
	}
	if !findings[0].FalsePositive {
		t.Error("expected FalsePositive flag to be true")
	}

	// Resolve
	if err := db.UpdateFindingStatus(f.ID, "resolved"); err != nil {
		t.Fatalf("UpdateFindingStatus failed: %v", err)
	}
	findings, _ = db.ListFindings("")
	if findings[0].Status != "resolved" {
		t.Errorf("expected resolved, got %s", findings[0].Status)
	}

	// Update nonexistent
	err = db.UpdateFindingStatus(999, "open")
	if err == nil {
		t.Error("expected error for nonexistent finding")
	}
}

func TestDeleteFinding(t *testing.T) {
	db := newTestDB(t)

	f := &FindingRecord{
		ConnectionName: "gh-main",
		RunID:          "42",
		Severity:       "medium",
		Category:       "config",
		Title:          "Insecure config",
		Description:    "Debug enabled",
		Status:         "open",
	}
	db.CreateFinding(f)

	if err := db.DeleteFinding(f.ID); err != nil {
		t.Fatalf("DeleteFinding failed: %v", err)
	}

	findings, _ := db.ListFindings("")
	if len(findings) != 0 {
		t.Errorf("expected 0 findings after delete, got %d", len(findings))
	}

	// Delete nonexistent
	err := db.DeleteFinding(999)
	if err == nil {
		t.Error("expected error for nonexistent finding")
	}
}

func TestGetFindingStats(t *testing.T) {
	db := newTestDB(t)

	db.CreateFinding(&FindingRecord{ConnectionName: "c1", RunID: "1", Severity: "critical", Category: "secrets", Title: "t1", Description: "d1", Status: "open"})
	db.CreateFinding(&FindingRecord{ConnectionName: "c1", RunID: "1", Severity: "critical", Category: "secrets", Title: "t2", Description: "d2", Status: "open"})
	db.CreateFinding(&FindingRecord{ConnectionName: "c1", RunID: "1", Severity: "high", Category: "config", Title: "t3", Description: "d3", Status: "open"})
	db.CreateFinding(&FindingRecord{ConnectionName: "c1", RunID: "1", Severity: "low", Category: "config", Title: "t4", Description: "d4", Status: "resolved"})

	stats, err := db.GetFindingStats()
	if err != nil {
		t.Fatalf("GetFindingStats failed: %v", err)
	}
	if stats["critical"] != 2 {
		t.Errorf("expected 2 critical, got %d", stats["critical"])
	}
	if stats["high"] != 1 {
		t.Errorf("expected 1 high, got %d", stats["high"])
	}
	if stats["open"] != 3 {
		t.Errorf("expected 3 open, got %d", stats["open"])
	}
	if stats["low"] != 1 {
		t.Errorf("expected 1 low, got %d", stats["low"])
	}
}

func TestAnalysisHistory(t *testing.T) {
	db := newTestDB(t)

	rec := &AnalysisRecord{
		ConnectionName: "gh-main",
		RunID:          "42",
		Summary:        "Found 3 issues",
		RiskScore:      65,
		FindingsCount:  3,
		TokensUsed:     2500,
		Model:          "claude-sonnet-4-20250514",
		DurationMS:     1500,
	}
	if err := db.CreateAnalysisRecord(rec); err != nil {
		t.Fatalf("CreateAnalysisRecord failed: %v", err)
	}
	if rec.ID == 0 {
		t.Error("expected non-zero ID")
	}

	// List all
	history, err := db.ListAnalysisHistory("")
	if err != nil {
		t.Fatalf("ListAnalysisHistory failed: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 record, got %d", len(history))
	}
	if history[0].RiskScore != 65 {
		t.Errorf("expected risk score 65, got %d", history[0].RiskScore)
	}

	// List by connection
	history, _ = db.ListAnalysisHistory("gh-main")
	if len(history) != 1 {
		t.Errorf("expected 1 record for gh-main, got %d", len(history))
	}
	history, _ = db.ListAnalysisHistory("nonexistent")
	if len(history) != 0 {
		t.Errorf("expected 0 records, got %d", len(history))
	}
}

func TestFindingsByConnection(t *testing.T) {
	db := newTestDB(t)

	db.CreateFinding(&FindingRecord{ConnectionName: "gh-main", RunID: "1", Severity: "high", Category: "secrets", Title: "t1", Description: "d1", Status: "open"})
	db.CreateFinding(&FindingRecord{ConnectionName: "gl-prod", RunID: "2", Severity: "low", Category: "config", Title: "t2", Description: "d2", Status: "open"})

	f1, _ := db.ListFindings("gh-main")
	if len(f1) != 1 {
		t.Errorf("expected 1 finding for gh-main, got %d", len(f1))
	}
	f2, _ := db.ListFindings("gl-prod")
	if len(f2) != 1 {
		t.Errorf("expected 1 finding for gl-prod, got %d", len(f2))
	}
	all, _ := db.ListFindings("")
	if len(all) != 2 {
		t.Errorf("expected 2 total findings, got %d", len(all))
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
