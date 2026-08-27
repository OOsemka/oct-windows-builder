package main

import "testing"

func TestSanitizeURLForLog(t *testing.T) {
	got := sanitizeURLForLog("https://user:secret@files.example.com:8443/win.iso?token=abc#x")
	want := "https://files.example.com:8443/win.iso"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if sanitizeURLForLog("") != "" {
		t.Fatal("empty")
	}
}

func TestValidDiskName(t *testing.T) {
	if !validDiskName("win2k19") || !validDiskName("win11") {
		t.Fatal("presets")
	}
	if validDiskName("Win2k19") || validDiskName("") || validDiskName("bad_name") {
		t.Fatal("invalid accepted")
	}
}
