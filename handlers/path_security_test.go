package handlers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	client "github.com/rancher/go-rancher/v2"
)

type archiveEntry struct {
	name     string
	typeflag byte
	body     string
	mode     int64
}

func encodedArchive(t *testing.T, entries ...archiveEntry) string {
	t.Helper()
	var content bytes.Buffer
	gzipWriter := gzip.NewWriter(&content)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		mode := entry.mode
		if mode == 0 {
			mode = 0600
		}
		header := &tar.Header{
			Name:     entry.name,
			Mode:     mode,
			Typeflag: entry.typeflag,
		}
		if entry.typeflag == 0 || entry.typeflag == tar.TypeReg || entry.typeflag == tar.TypeRegA {
			header.Size = int64(len(entry.body))
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := tarWriter.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(content.Bytes())
}

func TestMachinePathIdentifierPreservesLegitimateLegacyValues(t *testing.T) {
	for _, identifier := range []string{
		"1ph42",
		"host.example.com",
		"Machine_Name-01",
		"傳統主機名稱",
		"name with spaces",
	} {
		got, err := machinePathIdentifier(identifier)
		if err != nil {
			t.Fatalf("identifier %q was rejected: %v", identifier, err)
		}
		if got != identifier {
			t.Fatalf("identifier %q changed to %q", identifier, got)
		}
	}
}

func TestMachinePathIdentifierContainsTraversal(t *testing.T) {
	for _, identifier := range []string{
		"../outside",
		`..\outside`,
		"/tmp/outside",
		`C:\outside`,
		"bad\nname",
		strings.Repeat("a", 300),
	} {
		got, err := machinePathIdentifier(identifier)
		if err != nil {
			t.Fatalf("legacy identifier %q should receive a safe compatibility key: %v", identifier, err)
		}
		if !strings.HasPrefix(got, "legacy-") || strings.ContainsAny(got, `/\`) {
			t.Fatalf("identifier %q produced unsafe key %q", identifier, got)
		}
	}
	if _, err := machinePathIdentifier(""); err == nil {
		t.Fatal("empty identifier should be rejected")
	}
	if _, err := machinePathIdentifier("bad\x00name"); err == nil {
		t.Fatal("NUL-containing identifier should be rejected")
	}
}

func TestMachineCommandNamePreservesLegitimateNames(t *testing.T) {
	for _, name := range []string{"host-01", "host.example.com", "主機 一"} {
		got, err := machineCommandName(&client.Machine{Name: name, ExternalId: "1ph42"})
		if err != nil || got != name {
			t.Fatalf("legitimate name %q was not preserved: got=%q err=%v", name, got, err)
		}
	}
	got, err := machineCommandName(&client.Machine{ExternalId: "1ph42"})
	if err != nil || got != "1ph42" {
		t.Fatalf("empty optional name did not fall back to external identifier: got=%q err=%v", got, err)
	}
}

func TestMachineCommandNameRejectsPathAndOptionInjection(t *testing.T) {
	for _, name := range []string{"../outside", `..\outside`, "/tmp/outside", "-d", "bad\nname"} {
		if _, err := machineCommandName(&client.Machine{Name: name, ExternalId: "1ph42"}); err == nil {
			t.Fatalf("unsafe machine name %q was accepted", name)
		}
	}
}

func TestBuildBaseMachineDirCannotEscapeConfiguredRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MACHINE_WORK_DIR", root)
	t.Setenv("PASTURESTACK_HOME", "")
	t.Setenv("CATTLE_HOME", "")
	machine := &client.Machine{ExternalId: "../../outside"}

	dir, err := buildBaseMachineDir(machine)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(root, "machine", "machines")
	rel, err := filepath.Rel(wantRoot, dir)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		t.Fatalf("machine directory escaped storage root: root=%q dir=%q rel=%q err=%v", wantRoot, dir, rel, err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("safe machine directory was not created: %v", err)
	}
}

func TestRestoreMachineDirRejectsTraversalAndDoesNotWriteOutside(t *testing.T) {
	root := t.TempDir()
	baseDir := filepath.Join(root, "machine", "machines", "safe-id")
	machine := &client.Machine{ExtractedConfig: encodedArchive(t, archiveEntry{
		name: "../../outside.txt",
		body: "attacker-controlled",
	})}

	err := restoreMachineDir(machine, baseDir)
	if err == nil || !strings.Contains(err.Error(), "unsafe archive entry") {
		t.Fatalf("expected unsafe archive entry error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "outside.txt")); !os.IsNotExist(err) {
		t.Fatalf("archive wrote outside extraction root: %v", err)
	}
}

func TestRestoreMachineDirRejectsNestedDotDotEntry(t *testing.T) {
	root := t.TempDir()
	baseDir := filepath.Join(root, "machine", "machines", "safe-id")
	machine := &client.Machine{ExtractedConfig: encodedArchive(t, archiveEntry{
		name: "safe-id/../sibling.txt",
		body: "attacker-controlled",
	})}

	if err := restoreMachineDir(machine, baseDir); err == nil || !strings.Contains(err.Error(), "unsafe archive entry") {
		t.Fatalf("expected nested dot-dot entry to be rejected, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "machine", "machines", "sibling.txt")); !os.IsNotExist(err) {
		t.Fatalf("nested traversal entry was written: %v", err)
	}
}

func TestRestoreMachineDirRejectsCrossMachineEntry(t *testing.T) {
	root := t.TempDir()
	baseDir := filepath.Join(root, "machine", "machines", "safe-id")
	machine := &client.Machine{ExtractedConfig: encodedArchive(t, archiveEntry{
		name: "other-id/config.json",
		body: "attacker-controlled",
	})}

	if err := restoreMachineDir(machine, baseDir); err == nil || !strings.Contains(err.Error(), "does not belong to machine") {
		t.Fatalf("expected cross-machine entry to be rejected, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "machine", "machines", "other-id", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("cross-machine entry was written: %v", err)
	}
}

func TestRestoreMachineDirRejectsAbsoluteAndLinkEntries(t *testing.T) {
	for _, entry := range []archiveEntry{
		{name: "/tmp/outside.txt", body: "bad"},
		{name: `C:\outside.txt`, body: "bad"},
		{name: "link", typeflag: tar.TypeSymlink},
		{name: "device", typeflag: tar.TypeChar},
	} {
		t.Run(strings.ReplaceAll(entry.name, string(filepath.Separator), "_"), func(t *testing.T) {
			baseDir := filepath.Join(t.TempDir(), "machine", "machines", "safe-id")
			machine := &client.Machine{ExtractedConfig: encodedArchive(t, entry)}
			if err := restoreMachineDir(machine, baseDir); err == nil {
				t.Fatalf("unsafe entry %#v was accepted", entry)
			}
		})
	}
}

func TestRestoreMachineDirExtractsLegitimateNestedConfig(t *testing.T) {
	root := t.TempDir()
	baseDir := filepath.Join(root, "machine", "machines", "safe-id")
	machine := &client.Machine{ExtractedConfig: encodedArchive(t,
		archiveEntry{name: "safe-id/", typeflag: tar.TypeDir, mode: 0700},
		archiveEntry{name: "safe-id/config.json", body: `{"Driver":"example"}`},
	)}

	if err := restoreMachineDir(machine, baseDir); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, "machine", "machines", "safe-id", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != `{"Driver":"example"}` {
		t.Fatalf("unexpected restored content: %q", content)
	}
}

func TestAddDirToArchiveRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
	var content bytes.Buffer
	writer := tar.NewWriter(&content)
	if err := addDirToArchive(root, writer); err == nil {
		t.Fatal("symbolic link was archived")
	}
	_ = writer.Close()
}

func TestRemoveMachineDirRefusesParentDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MACHINE_WORK_DIR", root)
	marker := filepath.Join(root, "machine", "keep.txt")
	if err := os.MkdirAll(filepath.Dir(marker), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}

	removeMachineDir(filepath.Join(root, "machine"))
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("parent storage directory was removed: %v", err)
	}
}
