package pincer

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/igorsilveira/pincer/pkg/config"
	"github.com/spf13/cobra"
)

var backupCmd = &cobra.Command{
	Use:   "backup [output-path]",
	Short: "Snapshot the Pincer data directory to a tarball",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runBackup,
}

var restoreCmd = &cobra.Command{
	Use:   "restore <backup-path>",
	Short: "Restore Pincer state from a backup tarball",
	Args:  cobra.ExactArgs(1),
	RunE:  runRestore,
}

func runBackup(cmd *cobra.Command, args []string) error {
	dataDir := config.DataDir()
	if _, err := os.Stat(dataDir); err != nil {
		return fmt.Errorf("data directory %s does not exist", dataDir)
	}

	outPath := ""
	if len(args) > 0 {
		outPath = args[0]
	} else {
		outPath = fmt.Sprintf("pincer-backup-%s.tar.gz", time.Now().Format("20060102-150405"))
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating backup file: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	count := 0
	err = filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if strings.HasSuffix(path, "-wal") || strings.HasSuffix(path, "-shm") {
			return nil
		}

		relPath, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.Join("pincer-data", relPath)

		if err := tw.WriteHeader(header); err != nil {
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

		if _, err := io.Copy(tw, file); err != nil {
			return err
		}

		count++
		return nil
	})

	if err != nil {
		return fmt.Errorf("creating backup: %w", err)
	}

	fmt.Printf("Backup created: %s (%d files)\n", outPath, count)
	return nil
}

func runRestore(cmd *cobra.Command, args []string) error {
	backupPath := args[0]
	dataDir := config.DataDir()

	f, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("opening backup: %w", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("reading gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	count := 0
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}

		relPath := header.Name
		if idx := strings.Index(relPath, "/"); idx != -1 {
			relPath = relPath[idx+1:]
		}
		if relPath == "" || relPath == "." {
			continue
		}

		targetPath := filepath.Join(dataDir, relPath)

		if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(dataDir)) {
			return fmt.Errorf("invalid path in backup: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0700); err != nil {
				return fmt.Errorf("creating directory %s: %w", targetPath, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0700); err != nil {
				return err
			}
			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("creating file %s: %w", targetPath, err)
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return fmt.Errorf("writing file %s: %w", targetPath, err)
			}
			outFile.Close()
			count++
		}
	}

	fmt.Printf("Restored %d files to %s\n", count, dataDir)
	return nil
}
