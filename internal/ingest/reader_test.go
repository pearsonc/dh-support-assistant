package ingest

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestReadFileNotFound(t *testing.T) {
	t.Parallel()
	_, err := Read(filepath.Join(t.TempDir(), "does_not_exist.xlsx"))
	if err == nil || !strings.Contains(err.Error(), "open xlsx") {
		t.Fatalf("expected open error; got %v", err)
	}
}

func TestReadRejectsNoSheets(t *testing.T) {
	t.Parallel()
	// excelize refuses to save a workbook with zero sheets; the practical
	// equivalent is a corrupt file the library can't open, which Read
	// already surfaces via "open xlsx". Covered by TestReadFileNotFound.
	// This test takes the remaining branch: a workbook that opens but
	// whose first sheet is empty (no header row).
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	path := filepath.Join(t.TempDir(), "empty.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	_, err := Read(path)
	if err == nil || !strings.Contains(err.Error(), "no header row") {
		t.Fatalf("expected no-header-row error; got %v", err)
	}
}
