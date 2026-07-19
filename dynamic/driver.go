package dynamic

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/PastureStack/host-provisioner/logging"
	"github.com/pkg/errors"
)

var logger = logging.Logger()

const maxDriverDownloadSize int64 = 512 << 20

type Driver struct {
	builtin bool
	url     string
	hash    string
	name    string
}

func NewDriver(builtin bool, name, url, hash string) *Driver {
	d := &Driver{
		builtin: builtin,
		name:    name,
		url:     url,
		hash:    hash,
	}
	if d.builtin && !strings.HasPrefix(d.name, "docker-machine-driver-") {
		d.name = "docker-machine-driver-" + d.name
	}
	return d
}

func (d *Driver) Name() string {
	return d.name
}

func (d *Driver) Hash() string {
	return d.hash
}

func (d *Driver) Checksum() string {
	return d.name
}

func (d *Driver) FriendlyName() string {
	return strings.TrimPrefix(d.name, "docker-machine-driver-")
}

func (d *Driver) Remove() error {
	cacheRoot, err := openDriverCacheRoot()
	if err != nil {
		return err
	}
	defer cacheRoot.Close()

	key := d.cacheKey()
	driverName, err := isInstalled(cacheRoot, key)
	if err != nil || driverName == "" {
		return err
	}
	binRoot, err := openDriverBinRoot()
	if err != nil {
		return err
	}
	defer binRoot.Close()

	if err := removeIfPresent(binRoot, driverName); err != nil {
		return err
	}
	if err := removeIfPresent(cacheRoot, key+"-"+driverName); err != nil {
		return err
	}
	if err := removeIfPresent(cacheRoot, key); err != nil {
		return err
	}
	if err := removeIfPresent(cacheRoot, key+".error"); err != nil {
		return err
	}

	return nil
}

func (d *Driver) Stage() error {
	if err := d.getError(); err != nil {
		return err
	}

	return d.setError(d.stage())
}

func (d *Driver) setError(err error) error {
	if err == nil {
		return nil
	}
	cacheRoot, rootErr := openDriverCacheRoot()
	if rootErr != nil {
		return errors.Wrap(rootErr, err.Error())
	}
	defer cacheRoot.Close()
	if writeErr := cacheRoot.WriteFile(d.cacheKey()+".error", []byte(err.Error()), 0600); writeErr != nil {
		return errors.Wrap(writeErr, err.Error())
	}
	return err
}

func (d *Driver) getError() error {
	cacheRoot, err := openDriverCacheRoot()
	if err != nil {
		return err
	}
	defer cacheRoot.Close()
	errFile := d.cacheKey() + ".error"
	if content, err := cacheRoot.ReadFile(errFile); err == nil {
		logger.Error("Returning previous machine-driver error")
		removeIfPresent(cacheRoot, errFile)
		return errors.New(safeErrorMessage(string(content)))
	} else if !os.IsNotExist(err) {
		return err
	}

	return nil
}

func (d *Driver) ClearError() {
	cacheRoot, err := openDriverCacheRoot()
	if err != nil {
		logger.Errorf("Failed to open driver cache: %v", err)
		return
	}
	defer cacheRoot.Close()
	if err := removeIfPresent(cacheRoot, d.cacheKey()+".error"); err != nil {
		logger.Errorf("Failed to clear driver error: %v", err)
	}
}

func (d *Driver) stage() error {
	if d.builtin {
		return nil
	}

	cacheRoot, err := openDriverCacheRoot()
	if err != nil {
		return err
	}
	defer cacheRoot.Close()
	key := d.cacheKey()

	driverName, err := isInstalled(cacheRoot, key)
	if err != nil || driverName != "" {
		d.name = driverName
		return err
	}

	tempFile, err := ioutil.TempFile("", "machine-driver")
	if err != nil {
		return err
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	hasher, err := getHasher(d.hash)
	if err != nil {
		return err
	}

	downloadDest := io.Writer(tempFile)
	if hasher != nil {
		downloadDest = io.MultiWriter(tempFile, hasher)
	}

	if err := d.download(downloadDest); err != nil {
		return err
	}

	if got, ok := compare(hasher, d.hash); !ok {
		return fmt.Errorf("Hash does not match, got %s, expected %s", got, d.hash)
	}

	if err := tempFile.Close(); err != nil {
		return err
	}

	driverName, err = d.copyBinary(cacheRoot, key, tempFile.Name())
	if err != nil {
		return err
	}

	d.name = driverName
	return nil
}

func (d *Driver) Install() error {
	if d.builtin {
		return nil
	}

	driverName, err := safeDriverName(d.name)
	if err != nil {
		return err
	}
	cacheRoot, err := openDriverCacheRoot()
	if err != nil {
		return err
	}
	defer cacheRoot.Close()
	binRoot, err := openDriverBinRoot()
	if err != nil {
		return err
	}
	defer binRoot.Close()

	tmpName := driverName + "-tmp-" + d.cacheKey()[:12]
	removeIfPresent(binRoot, tmpName)
	f, err := binRoot.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return errors.Wrapf(err, "Couldn't open temporary driver %v for writing", tmpName)
	}
	defer removeIfPresent(binRoot, tmpName)

	srcName := d.cacheKey() + "-" + driverName
	src, err := cacheRoot.Open(srcName)
	if err != nil {
		f.Close()
		return errors.Wrapf(err, "Couldn't open cached driver %v for copying", driverName)
	}
	defer src.Close()

	logger.Infof("Installing driver %v", driverName)
	_, err = io.Copy(f, src)
	if err != nil {
		f.Close()
		return errors.Wrapf(err, "Couldn't copy cached driver %v", driverName)
	}
	if err := f.Close(); err != nil {
		return errors.Wrapf(err, "Couldn't close temporary driver %v", driverName)
	}
	if err := binRoot.Chmod(tmpName, 0755); err != nil {
		return errors.Wrapf(err, "Couldn't mark driver %v executable", driverName)
	}
	err = binRoot.Rename(tmpName, driverName)
	return errors.Wrapf(err, "Couldn't install driver %v", driverName)
}

func isElf(input string) bool {
	f, err := os.Open(input)
	if err != nil {
		return false
	}
	defer f.Close()

	elf := make([]byte, 4)
	if _, err := f.Read(elf); err != nil {
		return false
	}

	return bytes.Compare(elf, []byte{0x7f, 0x45, 0x4c, 0x46}) == 0
}

func (d *Driver) copyBinary(cacheRoot *os.Root, cacheKey, input string) (string, error) {
	if len(cacheKey) != sha256.Size*2 || strings.Trim(cacheKey, "0123456789abcdef") != "" {
		return "", fmt.Errorf("invalid driver cache key")
	}

	tempFile, err := ioutil.TempFile("", "machine-driver-binary")
	if err != nil {
		return "", err
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	var driverName string
	if isElf(input) {
		u, err := url.Parse(d.url)
		if err != nil {
			return "", err
		}
		driverName, err = safeDriverName(strings.Split(path.Base(u.Path), "_")[0])
		if err != nil {
			return "", fmt.Errorf("invalid driver URL path: %w", err)
		}
		source, err := os.Open(input)
		if err != nil {
			return "", err
		}
		defer source.Close()
		if err := copyWithLimit(tempFile, source); err != nil {
			return "", err
		}
	} else {
		driverName, err = copyDriverFromArchive(input, tempFile)
		if err != nil {
			return "", err
		}
	}

	if err := tempFile.Sync(); err != nil {
		return "", err
	}
	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		return "", err
	}

	destName := cacheKey + "-" + driverName
	removeIfPresent(cacheRoot, destName)
	dest, err := cacheRoot.OpenFile(destName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(dest, tempFile); err != nil {
		dest.Close()
		removeIfPresent(cacheRoot, destName)
		return "", err
	}
	if err := dest.Close(); err != nil {
		removeIfPresent(cacheRoot, destName)
		return "", err
	}
	if err := cacheRoot.WriteFile(cacheKey, []byte(driverName), 0600); err != nil {
		removeIfPresent(cacheRoot, destName)
		return "", err
	}

	logger.Infof("Found driver %s", driverName)
	return driverName, nil
}

func copyDriverFromArchive(input string, destination *os.File) (string, error) {
	if archive, err := zip.OpenReader(input); err == nil {
		defer archive.Close()
		return copyDriverFromZip(archive.File, destination)
	}

	archive, err := os.Open(input)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	var reader io.Reader = archive
	if gzipReader, gzipErr := gzip.NewReader(archive); gzipErr == nil {
		defer gzipReader.Close()
		reader = gzipReader
	} else if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return copyDriverFromTar(tar.NewReader(reader), destination)
}

func copyDriverFromZip(files []*zip.File, destination *os.File) (string, error) {
	found := ""
	for _, file := range files {
		entry, err := safeArchiveEntry(file.Name)
		if err != nil {
			return "", err
		}
		mode := file.Mode()
		if mode.IsDir() {
			continue
		}
		if !mode.IsRegular() {
			return "", fmt.Errorf("driver archive entry %q is not a regular file", file.Name)
		}
		name, err := safeDriverName(filepath.Base(entry))
		if err != nil {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("driver archive contains multiple driver binaries")
		}
		source, err := file.Open()
		if err != nil {
			return "", err
		}
		copyErr := copyWithLimit(destination, source)
		closeErr := source.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		found = name
	}
	if found == "" {
		return "", fmt.Errorf("driver archive contains no valid driver binary")
	}
	return found, nil
}

func copyDriverFromTar(reader *tar.Reader, destination *os.File) (string, error) {
	found := ""
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("invalid driver archive: %w", err)
		}
		entry, err := safeArchiveEntry(header.Name)
		if err != nil {
			return "", err
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return "", fmt.Errorf("driver archive entry %q is not a regular file", header.Name)
		}
		name, err := safeDriverName(filepath.Base(entry))
		if err != nil {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("driver archive contains multiple driver binaries")
		}
		if err := copyWithLimit(destination, reader); err != nil {
			return "", err
		}
		found = name
	}
	if found == "" {
		return "", fmt.Errorf("driver archive contains no valid driver binary")
	}
	return found, nil
}

func safeArchiveEntry(name string) (string, error) {
	if name == "" || filepath.IsAbs(name) || strings.Contains(name, `\`) || strings.Contains(name, "..") || !filepath.IsLocal(name) {
		return "", fmt.Errorf("unsafe driver archive entry %q", name)
	}
	return filepath.Clean(name), nil
}

func copyWithLimit(destination io.Writer, source io.Reader) error {
	written, err := io.Copy(destination, io.LimitReader(source, maxDriverDownloadSize+1))
	if err != nil {
		return err
	}
	if written > maxDriverDownloadSize {
		return fmt.Errorf("driver payload exceeds %d bytes", maxDriverDownloadSize)
	}
	return nil
}

func compare(hash hash.Hash, value string) (string, bool) {
	if hash == nil {
		return "", true
	}

	got := hex.EncodeToString(hash.Sum([]byte{}))
	expected := strings.TrimSpace(strings.ToLower(value))

	return got, got == expected
}

func getHasher(hash string) (hash.Hash, error) {
	switch len(hash) {
	case 64:
		return sha256.New(), nil
	case 128:
		return sha512.New(), nil
	}

	return nil, fmt.Errorf("machine-driver checksum must be SHA-256 or SHA-512")
}

func (d *Driver) download(dest io.Writer) error {
	u, err := url.Parse(d.url)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("invalid driver download URL")
	}
	logger.Infof("Downloading machine driver from host %q", u.Hostname())
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(u.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("driver download returned HTTP %d", resp.StatusCode)
	}

	return copyWithLimit(dest, resp.Body)
}

func (d *Driver) cacheKey() string {
	return sha256Bytes([]byte(d.url + d.hash))
}

func trustedStorageRoot(value, fallback string) (string, error) {
	if value == "" {
		value = fallback
	}
	if strings.IndexByte(value, 0) >= 0 || !filepath.IsAbs(value) {
		return "", fmt.Errorf("storage root must be an absolute local path")
	}
	root, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	if strings.IndexByte(root, 0) >= 0 || !filepath.IsAbs(root) || !strings.HasPrefix(root, string(filepath.Separator)) {
		return "", fmt.Errorf("storage root must be an absolute local path")
	}
	return root, nil
}

func driverCacheDir() (string, error) {
	base := os.Getenv("PASTURESTACK_HOME")
	if base == "" {
		base = os.Getenv("CATTLE_HOME")
	}
	base, err := trustedStorageRoot(base, "/var/lib/pasturestack")
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "machine-drivers"), nil
}

func driverBinDir() (string, error) {
	return trustedStorageRoot(os.Getenv("GMS_BIN_DIR"), "/usr/local/bin")
}

func openDriverCacheRoot() (*os.Root, error) {
	dir, err := driverCacheDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return os.OpenRoot(dir)
}

func openDriverBinRoot() (*os.Root, error) {
	dir, err := driverBinDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return os.OpenRoot(dir)
}

func safeDriverName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if len(name) <= len("docker-machine-driver-") || len(name) > 255 || !strings.HasPrefix(name, "docker-machine-driver-") ||
		strings.IndexByte(name, 0) >= 0 || strings.HasPrefix(name, "-") || !filepath.IsLocal(name) || filepath.Base(name) != name ||
		strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("invalid machine driver name %q", name)
	}
	for _, char := range name {
		if char <= 0x1f || char == 0x7f {
			return "", fmt.Errorf("invalid machine driver name %q", name)
		}
	}
	return name, nil
}

func isInstalled(root *os.Root, key string) (string, error) {
	if len(key) != sha256.Size*2 || strings.Trim(key, "0123456789abcdef") != "" {
		return "", fmt.Errorf("invalid driver cache key")
	}
	content, err := root.ReadFile(key)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return safeDriverName(string(content))
}

func removeIfPresent(root *os.Root, name string) error {
	err := root.Remove(name)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func safeErrorMessage(message string) string {
	message = strings.TrimSpace(message)
	var clean strings.Builder
	for _, char := range message {
		if char < 0x20 || char == 0x7f {
			clean.WriteByte(' ')
		} else {
			clean.WriteRune(char)
		}
		if clean.Len() >= 4096 {
			break
		}
	}
	return clean.String()
}

func sha256Bytes(content []byte) string {
	hash := sha256.New()
	io.Copy(hash, bytes.NewBuffer(content))
	return hex.EncodeToString(hash.Sum([]byte{}))
}
