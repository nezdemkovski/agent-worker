package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestEmitJSON(t *testing.T) {
	output := captureStdout(t, func() {
		emit(map[string]string{"status": "ok", "pid": "123"}, outputMode{json: true})
	})
	if !strings.Contains(output, `"status":"ok"`) {
		t.Fatalf("expected JSON status field, got %q", output)
	}
	if !strings.Contains(output, `"pid":"123"`) {
		t.Fatalf("expected JSON pid field, got %q", output)
	}
}

func TestEmitKeyValue(t *testing.T) {
	output := captureStdout(t, func() {
		emit(map[string]string{"status": "ok", "pid": "123"}, outputMode{})
	})
	if !strings.Contains(output, "status=ok") {
		t.Fatalf("expected key=value status, got %q", output)
	}
	if !strings.Contains(output, "pid=123") {
		t.Fatalf("expected key=value pid, got %q", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(): %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("w.Close(): %v", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy(): %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("r.Close(): %v", err)
	}
	return buf.String()
}
