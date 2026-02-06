package pullpreview

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func CreateTarball(srcDir string) (string, func(), error) {
	abs, err := filepath.Abs(srcDir)
	if err != nil {
		return "", nil, err
	}
	file, err := os.CreateTemp("", "pullpreview-app-*.tar.gz")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = os.Remove(file.Name())
	}

	gz := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gz)

	walkErr := filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(rel, ".git") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			header.Linkname = link
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(tarWriter, f)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
		return nil
	})

	closeErr := tarWriter.Close()
	gzipErr := gz.Close()
	fileErr := file.Close()

	if walkErr != nil {
		return "", cleanup, walkErr
	}
	if closeErr != nil {
		return "", cleanup, closeErr
	}
	if gzipErr != nil {
		return "", cleanup, gzipErr
	}
	if fileErr != nil {
		return "", cleanup, fileErr
	}
	return file.Name(), cleanup, nil
}
