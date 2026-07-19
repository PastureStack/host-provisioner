package dynamic

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeDriverName(t *testing.T) {
	valid := "docker-machine-driver-example"
	if got, err := safeDriverName(valid); err != nil || got != valid {
		t.Fatalf("valid driver name rejected: got %q, err %v", got, err)
	}

	invalid := []string{
		"example",
		"docker-machine-driver-",
		"../docker-machine-driver-escape",
		"docker-machine-driver-../../escape",
		"docker-machine-driver-sub/name",
		"docker-machine-driver-sub\\name",
		"docker-machine-driver-bad\nname",
	}
	for _, name := range invalid {
		t.Run(strings.ReplaceAll(name, "/", "_"), func(t *testing.T) {
			if _, err := safeDriverName(name); err == nil {
				t.Fatalf("unsafe driver name %q was accepted", name)
			}
		})
	}
}

func TestRemoveRejectsPoisonedCacheName(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	t.Setenv("PASTURESTACK_HOME", home)
	t.Setenv("CATTLE_HOME", "")
	t.Setenv("GMS_BIN_DIR", bin)
	driver := NewDriver(false, "", "https://example.invalid/driver", strings.Repeat("a", 64))

	cacheDir := filepath.Join(home, "machine-drivers")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, driver.cacheKey()), []byte("../../victim"), 0600); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(home, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := driver.Remove(); err == nil {
		t.Fatal("poisoned cache metadata was accepted")
	}
	if got, err := os.ReadFile(victim); err != nil || string(got) != "keep" {
		t.Fatalf("outside file changed: content %q, err %v", got, err)
	}
}

func TestInstallRejectsTraversalDriverName(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	t.Setenv("PASTURESTACK_HOME", home)
	t.Setenv("CATTLE_HOME", "")
	t.Setenv("GMS_BIN_DIR", bin)
	driver := NewDriver(false, "docker-machine-driver-../../escape", "https://example.invalid/driver", strings.Repeat("b", 64))
	if err := driver.Install(); err == nil {
		t.Fatal("traversal driver name was accepted")
	}
	entries, err := os.ReadDir(bin)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unsafe install wrote files: %v", entries)
	}
}

func TestTarDriverArchiveRejectsUnsafeEntries(t *testing.T) {
	for _, entry := range []struct {
		name     string
		typeflag byte
	}{
		{name: "../../docker-machine-driver-escape", typeflag: tar.TypeReg},
		{name: "/docker-machine-driver-escape", typeflag: tar.TypeReg},
		{name: "docker-machine-driver-link", typeflag: tar.TypeSymlink},
		{name: "docker-machine-driver-device", typeflag: tar.TypeChar},
	} {
		t.Run(strings.ReplaceAll(entry.name, "/", "_"), func(t *testing.T) {
			var archive bytes.Buffer
			writer := tar.NewWriter(&archive)
			header := &tar.Header{Name: entry.name, Typeflag: entry.typeflag, Mode: 0755, Size: 3}
			if entry.typeflag != tar.TypeReg {
				header.Size = 0
			}
			if err := writer.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
			if header.Size > 0 {
				if _, err := writer.Write([]byte("elf")); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}

			destination, err := os.CreateTemp(t.TempDir(), "driver")
			if err != nil {
				t.Fatal(err)
			}
			defer destination.Close()
			if _, err := copyDriverFromTar(tar.NewReader(bytes.NewReader(archive.Bytes())), destination); err == nil {
				t.Fatalf("unsafe archive entry %q was accepted", entry.name)
			}
		})
	}
}

func TestZipDriverArchiveRejectsTraversal(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	file, err := writer.Create("../../docker-machine-driver-escape")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("elf")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	destination, err := os.CreateTemp(t.TempDir(), "driver")
	if err != nil {
		t.Fatal(err)
	}
	defer destination.Close()
	if _, err := copyDriverFromZip(reader.File, destination); err == nil {
		t.Fatal("zip traversal entry was accepted")
	}
}

func TestTarDriverArchiveCopiesOneLegitimateBinary(t *testing.T) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	payload := []byte("\x7fELF-reviewed-driver")
	if err := writer.WriteHeader(&tar.Header{
		Name:     "release/docker-machine-driver-example",
		Typeflag: tar.TypeReg,
		Mode:     0755,
		Size:     int64(len(payload)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	destination, err := os.CreateTemp(t.TempDir(), "driver")
	if err != nil {
		t.Fatal(err)
	}
	defer destination.Close()
	name, err := copyDriverFromTar(tar.NewReader(bytes.NewReader(archive.Bytes())), destination)
	if err != nil {
		t.Fatal(err)
	}
	if name != "docker-machine-driver-example" {
		t.Fatalf("unexpected driver name %q", name)
	}
	if _, err := destination.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("copied payload mismatch: %q", got)
	}
}

func TestStorageRootsMustBeAbsolute(t *testing.T) {
	if _, err := trustedStorageRoot("relative/path", "/unused"); err == nil {
		t.Fatal("relative storage root was accepted")
	}
}

func TestWeakOrMissingDriverChecksumsAreRejected(t *testing.T) {
	for _, checksum := range []string{"", strings.Repeat("a", 32), strings.Repeat("b", 40), "not-a-checksum"} {
		if _, err := getHasher(checksum); err == nil {
			t.Fatalf("weak or missing checksum %q was accepted", checksum)
		}
	}
	if _, err := getHasher(strings.Repeat("c", 64)); err != nil {
		t.Fatalf("SHA-256 checksum was rejected: %v", err)
	}
}

func TestStageAndInstallLegitimateDriver(t *testing.T) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	payload := []byte("\x7fELF-reviewed-driver")
	if err := writer.WriteHeader(&tar.Header{
		Name:     "release/docker-machine-driver-example",
		Typeflag: tar.TypeReg,
		Mode:     0755,
		Size:     int64(len(payload)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/x-tar")
		_, _ = response.Write(archive.Bytes())
	}))
	defer server.Close()
	digest := sha256.Sum256(archive.Bytes())
	home := t.TempDir()
	bin := t.TempDir()
	t.Setenv("PASTURESTACK_HOME", home)
	t.Setenv("CATTLE_HOME", "")
	t.Setenv("GMS_BIN_DIR", bin)
	driver := NewDriver(false, "", server.URL+"/docker-machine-driver-example.tar", fmt.Sprintf("%x", digest))

	if err := driver.Stage(); err != nil {
		t.Fatalf("legitimate driver stage failed: %v", err)
	}
	if driver.Name() != "docker-machine-driver-example" {
		t.Fatalf("unexpected staged driver name %q", driver.Name())
	}
	if err := driver.Install(); err != nil {
		t.Fatalf("legitimate driver install failed: %v", err)
	}
	installed, err := os.ReadFile(filepath.Join(bin, "docker-machine-driver-example"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(installed, payload) {
		t.Fatalf("installed driver mismatch: %q", installed)
	}
}
