package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func extractArchive(path, dest string) error {
	if strings.HasSuffix(strings.ToLower(path), ".zip") {
		return extractZip(path, dest)
	}
	return extractTarGz(path, dest)
}

func extractTarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := safeJoin(dest, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(name, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				return err
			}
			of, err := os.Create(name)
			if err != nil {
				return err
			}
			if _, err := io.Copy(of, tr); err != nil {
				_ = of.Close()
				return err
			}
			if err := of.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			_ = os.MkdirAll(filepath.Dir(name), 0o755)
			if err := os.Symlink(hdr.Linkname, name); err != nil {
				return err
			}
		}
	}
	return nil
}

func extractZip(src, dest string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := safeJoin(dest, f.Name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(name, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		of, err := os.Create(name)
		if err != nil {
			_ = rc.Close()
			return err
		}
		if _, err := io.Copy(of, rc); err != nil {
			_ = rc.Close()
			_ = of.Close()
			return err
		}
		_ = rc.Close()
		if err := of.Close(); err != nil {
			return err
		}
	}
	return nil
}

// safeJoin joins dest and name, collapsing any ".." traversal.
func safeJoin(dest, name string) string {
	return filepath.Join(dest, strings.TrimPrefix(filepath.Clean("/"+name), "/"))
}

// createTarGz archives the given absolute paths into dest, storing absolute
// entry names so extraction to "/" restores the original locations.
func createTarGz(paths []string, dest string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	for _, p := range paths {
		if err := addPathToTar(tw, p); err != nil {
			return err
		}
	}
	return nil
}

func addPathToTar(tw *tar.Writer, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = path
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			hdr.Linkname = target
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.IsDir() && info.Mode().IsRegular() {
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, in); err != nil {
				_ = in.Close()
				return err
			}
			_ = in.Close()
		}
		return nil
	})
}
