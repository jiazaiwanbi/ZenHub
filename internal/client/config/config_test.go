package config

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDefaultFileBuildsRuntime(t *testing.T) {
	file := DefaultFile()

	runtime, err := file.Runtime()
	if err != nil {
		t.Fatalf("DefaultFile().Runtime() error = %v", err)
	}
	if runtime.Listen == "" {
		t.Fatal("DefaultFile().Runtime().Listen is empty")
	}
	if len(runtime.Routes) == 0 {
		t.Fatal("DefaultFile().Runtime().Routes is empty")
	}
	if len(runtime.ProviderGroups) == 0 {
		t.Fatal("DefaultFile().Runtime().ProviderGroups is empty")
	}
}

func TestEnsureDefaultConfigCreatesStarterFiles(t *testing.T) {
	paths := clientPathsFromRoot(t.TempDir())

	created, err := ensureDefaultConfig(paths)
	if err != nil {
		t.Fatalf("ensureDefaultConfig() error = %v", err)
	}
	if !created {
		t.Fatal("ensureDefaultConfig() created = false, want true")
	}

	file, err := LoadFile(paths.ConfigPath)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if _, err := file.Runtime(); err != nil {
		t.Fatalf("starter config Runtime() error = %v", err)
	}

	if _, err := os.Stat(paths.LogDir); err != nil {
		t.Fatalf("os.Stat(log dir) error = %v", err)
	}

	configInfo, err := os.Stat(paths.ConfigPath)
	if err != nil {
		t.Fatalf("os.Stat(config) error = %v", err)
	}
	if got := configInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("config permissions = %v, want 0600", got)
	}

	configDirInfo, err := os.Stat(paths.ConfigDir)
	if err != nil {
		t.Fatalf("os.Stat(config dir) error = %v", err)
	}
	if got := configDirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("config dir permissions = %v, want 0700", got)
	}

	logDirInfo, err := os.Stat(paths.LogDir)
	if err != nil {
		t.Fatalf("os.Stat(log dir) error = %v", err)
	}
	if got := logDirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("log dir permissions = %v, want 0700", got)
	}
}

func TestPathsForConfigReturnsAbsolutePath(t *testing.T) {
	relative := filepath.Join("testdata", "..", "client.json")

	paths, err := PathsForConfig(relative)
	if err != nil {
		t.Fatalf("PathsForConfig() error = %v", err)
	}
	if !filepath.IsAbs(paths.ConfigPath) {
		t.Fatalf("ConfigPath = %q, want absolute path", paths.ConfigPath)
	}
	if filepath.Base(paths.LogDir) != "logs" {
		t.Fatalf("LogDir = %q, want logs suffix", paths.LogDir)
	}
	if paths.SyncStatePath != SyncStatePath(paths.ConfigPath) {
		t.Fatalf("SyncStatePath = %q, want %q", paths.SyncStatePath, SyncStatePath(paths.ConfigPath))
	}
}

func TestWriteFileAtomicCreatesSecureFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	if err := WriteFileAtomic(path, []byte("{\"ok\":true}\n"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("created file permissions = %v, want 0600", got)
	}
}

func TestWriteFileAtomicPreservesExistingFileOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission failure semantics differ on Windows")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := []byte("{\"listen\":\"127.0.0.1:8080\"}\n")

	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("os.WriteFile(original) error = %v", err)
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("os.Chmod(read-only dir) error = %v", err)
	}
	defer func() {
		_ = os.Chmod(dir, 0o700)
	}()

	err := WriteFileAtomic(path, []byte("{\"listen\":\"127.0.0.1:9090\"}\n"), 0o600)
	if err == nil {
		t.Fatal("WriteFileAtomic() error = nil, want failure in read-only directory")
	}

	current, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("os.ReadFile() error = %v", readErr)
	}
	if !bytes.Equal(current, original) {
		t.Fatalf("file content changed after failed atomic write = %q, want %q", current, original)
	}
}
