package handlers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	b64 "encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/PastureStack/host-provisioner/logging"
	client "github.com/rancher/go-rancher/v2"
	"github.com/sirupsen/logrus"
)

var logger = logging.Logger()

func restoreMachineDir(machine *client.Machine, baseDir string) error {
	machineBaseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("resolve machine config directory: %w", err)
	}
	if err := os.MkdirAll(machineBaseDir, 0740); err != nil {
		return fmt.Errorf("Error reinitializing config (MkdirAll). Config Dir: %v. Error: %v", machineBaseDir, err)
	}

	if machine.ExtractedConfig == "" {
		return nil
	}

	configBytes, err := b64.StdEncoding.DecodeString(machine.ExtractedConfig)
	if err != nil {
		return fmt.Errorf("Error reinitializing config (base64.DecodeString). Config Dir: %v. Error: %v", machineBaseDir, err)
	}

	gzipReader, err := gzip.NewReader(bytes.NewReader(configBytes))
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	root, err := os.OpenRoot(machineBaseDir)
	if err != nil {
		return fmt.Errorf("open machine config root: %w", err)
	}
	defer root.Close()
	archiveRoot := filepath.Base(machineBaseDir)
	sawArchiveRoot := false

	for {
		header, err := tarReader.Next()
		if err != nil {
			if err == io.EOF {
				if !sawArchiveRoot {
					return fmt.Errorf("archive is missing machine root %q", archiveRoot)
				}
				return nil
			}
			return fmt.Errorf("Error reinitializing config (tarRead.Next). Config Dir: %v. Error: %v", machineBaseDir, err)
		}

		if header.Name == "" || filepath.IsAbs(header.Name) || strings.Contains(header.Name, "..") || strings.Contains(header.Name, `\`) {
			return fmt.Errorf("unsafe archive entry %q", header.Name)
		}
		if !filepath.IsLocal(header.Name) {
			return fmt.Errorf("unsafe archive entry %q", header.Name)
		}
		filename := filepath.Clean(header.Name)
		if filename != archiveRoot && !strings.HasPrefix(filename, archiveRoot+string(filepath.Separator)) {
			return fmt.Errorf("archive entry %q does not belong to machine %q", header.Name, archiveRoot)
		}
		sawArchiveRoot = true
		filename = strings.TrimPrefix(filename, archiveRoot)
		filename = strings.TrimPrefix(filename, string(filepath.Separator))
		if filename == "" {
			if !header.FileInfo().IsDir() {
				return fmt.Errorf("archive root %q is not a directory", header.Name)
			}
			continue
		}
		filePath := filepath.Join(machineBaseDir, filename)
		logger.Infof("Extracting %v", filePath)

		info := header.FileInfo()
		mode := info.Mode()
		if mode&os.ModeType != 0 && !mode.IsDir() {
			return fmt.Errorf("unsupported archive entry type for %q", header.Name)
		}
		if info.IsDir() {
			err = root.MkdirAll(filename, os.FileMode(header.Mode)&0777)
			if err != nil {
				return fmt.Errorf("Error reinitializing config (Mkdirall). Config Dir: %v. Dir: %v. Error: %v", machineBaseDir, info.Name(), err)
			}
			continue
		}

		parent := filepath.Dir(filename)
		if parent != "." {
			if err := root.MkdirAll(parent, 0740); err != nil {
				return fmt.Errorf("create archive parent for %q: %w", header.Name, err)
			}
		}
		file, err := root.OpenFile(filename, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return fmt.Errorf("Error reinitializing config (OpenFile). Config Dir: %v. File: %v. Error: %v", machineBaseDir, info.Name(), err)
		}
		_, err = io.Copy(file, tarReader)
		closeErr := file.Close()
		if err != nil {
			return fmt.Errorf("Error reinitializing config (Copy). Config Dir: %v. File: %v. Error: %v", machineBaseDir, info.Name(), err)
		}
		if closeErr != nil {
			return fmt.Errorf("Error reinitializing config (Close). Config Dir: %v. File: %v. Error: %v", machineBaseDir, info.Name(), closeErr)
		}
	}
}

func createExtractedConfig(baseDir string, machine *client.Machine) (string, error) {
	logger.WithFields(logrus.Fields{
		"resourceId": machine.Id,
	}).Info("Creating and uploading extracted machine config")

	// create the tar.gz file
	archiveName, err := machineStorageName(machine)
	if err != nil {
		return "", err
	}
	baseDir, err = filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolve machine config source: %w", err)
	}
	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	destName := archiveName + ".tar.gz"
	destFile := filepath.Join(baseDir, destName)
	tarfile, err := root.OpenFile(destName, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer tarfile.Close()
	fileWriter := gzip.NewWriter(tarfile)
	defer fileWriter.Close()
	tarfileWriter := tar.NewWriter(fileWriter)
	defer tarfileWriter.Close()

	if err := addDirToArchive(baseDir, tarfileWriter); err != nil {
		return "", err
	}

	return destFile, nil
}

func addDirToArchive(source string, tarfileWriter *tar.Writer) error {
	baseDir := filepath.Base(source)

	return filepath.Walk(source,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if path == source || strings.HasSuffix(info.Name(), ".iso") ||
				strings.HasSuffix(info.Name(), ".tar.gz") ||
				strings.HasSuffix(info.Name(), ".vmdk") ||
				strings.HasSuffix(info.Name(), ".img") {
				return nil
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing to archive symbolic link %q", path)
			}
			if !info.Mode().IsRegular() && !info.IsDir() {
				return fmt.Errorf("refusing to archive special file %q", path)
			}

			header, err := tar.FileInfoHeader(info, info.Name())
			if err != nil {
				return err
			}

			header.Name = filepath.Join(baseDir, strings.TrimPrefix(path, source))

			if err := tarfileWriter.WriteHeader(header); err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(tarfileWriter, file)
			return err
		})
}

func encodeFile(destFile string) (string, error) {
	extractedTarfile, err := ioutil.ReadFile(destFile)
	if err != nil {
		return "", err
	}

	extractedEncodedConfig := b64.StdEncoding.EncodeToString(extractedTarfile)
	if err != nil {
		return "", err
	}

	return extractedEncodedConfig, nil
}

func saveMachineConfig(machineDir string, machine *client.Machine, apiClient *client.RancherClient) error {
	var err error
	destFile, err := createExtractedConfig(machineDir, machine)
	if err != nil {
		return err
	}

	extractedConf, err := encodeFile(destFile)
	if err != nil {
		return err
	}

	for i := 0; i < 3; i++ {
		_, err = apiClient.Machine.Update(machine, &client.Machine{
			ExtractedConfig: extractedConf,
		})
		if err == nil {
			return err
		}
	}
	return err
}

func removeMachineDir(machineDir string) {
	workDir, err := trustedWorkDir()
	if err != nil {
		logger.WithError(err).Warn("Refusing to remove unresolved machine directory")
		return
	}
	machinesDir := filepath.Join(workDir, "machines")
	rel, err := filepath.Rel(machinesDir, machineDir)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		logger.WithField("machineDir", machineDir).Warn("Refusing to remove machine directory outside storage root")
		return
	}
	root, err := os.OpenRoot(machinesDir)
	if err != nil {
		logger.WithError(err).Warn("Refusing to remove machine directory without a trusted root")
		return
	}
	defer root.Close()
	if err := root.RemoveAll(rel); err != nil {
		logger.WithError(err).Warn("Unable to remove machine directory")
	}
}
