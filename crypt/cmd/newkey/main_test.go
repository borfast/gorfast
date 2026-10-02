package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/borfast/gorfast/crypt"
)

func TestDefaultIsXChaCha(t *testing.T) {
	var out, errb bytes.Buffer
	if status := run(nil, &out, &errb); status != 0 {
		t.Fatalf("run returned %d: %s", status, errb.String())
	}
	got := out.String()
	if !strings.HasPrefix(got, "xchacha20poly1305:") || !strings.HasSuffix(got, "\n") || strings.Count(got, "\n") != 1 {
		t.Fatal("output did not contain exactly one xchacha20poly1305 line")
	}
	if _, err := crypt.ParseKey(strings.TrimSuffix(got, "\n")); err != nil {
		t.Fatalf("output is not a valid key: %v", err)
	}
}

func TestAESFlag(t *testing.T) {
	var out, errb bytes.Buffer
	if status := run([]string{"-alg", "aes256gcm"}, &out, &errb); status != 0 {
		t.Fatalf("run returned %d: %s", status, errb.String())
	}
	got := out.String()
	if !strings.HasPrefix(got, "aes256gcm:") || !strings.HasSuffix(got, "\n") || strings.Count(got, "\n") != 1 {
		t.Fatal("output did not contain exactly one aes256gcm line")
	}
	if _, err := crypt.ParseKey(strings.TrimSuffix(got, "\n")); err != nil {
		t.Fatalf("output is not a valid key: %v", err)
	}
}

func TestEachRunDiffers(t *testing.T) {
	var first, second, errb bytes.Buffer
	if status := run(nil, &first, &errb); status != 0 {
		t.Fatalf("first run returned %d: %s", status, errb.String())
	}
	if status := run(nil, &second, &errb); status != 0 {
		t.Fatalf("second run returned %d: %s", status, errb.String())
	}
	if first.String() == second.String() {
		t.Fatal("two runs generated the same key")
	}
}

func TestBadUsage(t *testing.T) {
	var out, errb bytes.Buffer
	if status := run([]string{"-alg", "des"}, &out, &errb); status != 2 {
		t.Fatalf("invalid algorithm returned %d", status)
	}
	if out.Len() != 0 {
		t.Fatalf("invalid algorithm wrote to stdout: %q", out.String())
	}
	if !strings.Contains(errb.String(), "xchacha20poly1305") || !strings.Contains(errb.String(), "aes256gcm") {
		t.Fatalf("usage did not list supported algorithms: %q", errb.String())
	}

	out.Reset()
	errb.Reset()
	if status := run([]string{"extra"}, &out, &errb); status != 2 {
		t.Fatalf("extra argument returned %d", status)
	}
	if out.Len() != 0 {
		t.Fatalf("extra argument wrote to stdout: %q", out.String())
	}
}
