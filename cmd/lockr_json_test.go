package cmd

import (
	"encoding/json"
	"testing"
)

// lockr's real stdout when not a terminal: its spinner's escape codes, then the JSON.
const lockrSpinner = "\x1b[?25l\x1b[?2004h\x1b]11;?\x1b\\⣽ Fetching secrets...⣻ Fetching secrets...\x1b[2K\x1b[?2004l\x1b[?25h"

func TestLockrJSON(t *testing.T) {
	list := lockrSpinner + "[\n  {\n    \"name\": \"/acct/use1/vpc/services/vpc_id\"\n  }\n]\n"
	var items []struct{ Name string }
	if err := json.Unmarshal(lockrJSON([]byte(list)), &items); err != nil || len(items) != 1 || items[0].Name != "/acct/use1/vpc/services/vpc_id" {
		t.Fatalf("list behind a spinner: %v %v", items, err)
	}

	read := lockrSpinner + "{\"value\": \"vpc-123\"}\n"
	var secret lockrSecret
	if err := json.Unmarshal(lockrJSON([]byte(read)), &secret); err != nil || secret.Value != "vpc-123" {
		t.Fatalf("read behind a spinner: %v %v", secret, err)
	}

	for _, plain := range []string{"[]", "{\"value\": \"x\"}"} {
		if got := string(lockrJSON([]byte(plain))); got != plain {
			t.Errorf("plain JSON changed: %q -> %q", plain, got)
		}
	}
}
