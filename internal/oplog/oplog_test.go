package oplog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "operations.jsonl")
	logger := New(path)
	entry := Entry{Time: time.Now(), Action: "delete", ID: "abc", FolderPath: "C:\\tmp\\a", Success: true}
	if err := logger.Append(entry); err != nil {
		t.Fatal(err)
	}
	if err := logger.Append(entry); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		var decoded Entry
		if err := json.Unmarshal(scanner.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.ID != "abc" || !decoded.Success {
			t.Fatalf("decoded = %+v", decoded)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("lines = %d", count)
	}
}
