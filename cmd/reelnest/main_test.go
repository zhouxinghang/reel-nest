package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOptionalMetadataSecrets(t *testing.T) {
	for _, name := range []string{"TMDB_READ_ACCESS_TOKEN", "TVDB_API_KEY", "TVDB_PIN", "METATUBE_TOKEN"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "  environment-secret  ")
			t.Setenv(name+"_FILE", "")
			if optionalSecret(name) != "environment-secret" {
				t.Fatal("environment credential not loaded")
			}
			path := filepath.Join(t.TempDir(), "credential")
			if err := os.WriteFile(path, []byte("  file-secret\n"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv(name+"_FILE", path)
			if optionalSecret(name) != "file-secret" {
				t.Fatal("file credential did not take precedence")
			}
			t.Setenv(name+"_FILE", path+"-missing")
			if optionalSecret(name) != "" {
				t.Fatal("unreadable optional file must disable the credential, not use environment fallback")
			}
		})
	}
}
