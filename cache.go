package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type CacheService struct {
	GitHubClient interface {
		GetCacheEntry(ctx context.Context, key, version string) (*CacheEntryResponse, error)
		ReserveCache(ctx context.Context, key, version string) (*ReserveCacheResponse, error)
		UploadCache(ctx context.Context, id CacheID, r io.Reader) error
		CommitCache(ctx context.Context, id CacheID, size int64) error
	}
	HTTPClient *http.Client
}

// cacheVersion is the service-side version discriminator for a set of paths.
func cacheVersion(paths []string) string {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, "\x00")))
	return hex.EncodeToString(h[:])
}

// RestoreCache restores a previously saved module cache.
// Returns the matched key, or "" on a miss.
func (s CacheService) RestoreCache(ctx context.Context, paths []string, primaryKey string) (string, error) {
	entry, err := s.GitHubClient.GetCacheEntry(ctx, primaryKey, cacheVersion(paths))
	if err != nil {
		return "", err
	}
	if entry == nil {
		return "", nil // cache miss
	}

	archive, err := os.CreateTemp("", "gh-setup-go-restore-*.tar.gz")
	if err != nil {
		return "", err
	}
	defer os.Remove(archive.Name())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.ArchiveLocation, nil)
	if err != nil {
		return "", err
	}
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", &ErrHTTP{StatusCode: resp.StatusCode}
	}

	if _, err := io.Copy(archive, resp.Body); err != nil {
		return "", err
	}

	if err := archive.Close(); err != nil {
		return "", err
	}

	if err := extractArchive(archive.Name(), "/"); err != nil {
		return "", err
	}

	return primaryKey, nil
}

func (s CacheService) SaveCache(ctx context.Context, paths []string, primaryKey string) error {
	archive, err := os.CreateTemp("", "gh-setup-go-save-*.tar.gz")
	if err != nil {
		return err
	}
	archivePath := archive.Name()
	if err := archive.Close(); err != nil {
		return err
	}
	defer os.Remove(archivePath)
	if err := createTarGz(paths, archivePath); err != nil {
		return err
	}

	res, err := s.GitHubClient.ReserveCache(ctx, primaryKey, cacheVersion(paths))
	if err != nil {
		return err
	}

	// upload chunks (streamed sequentially by the client)
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := s.GitHubClient.UploadCache(ctx, res.CacheID, f); err != nil {
		return err
	}

	info, err := f.Stat()
	if err != nil {
		return err
	}
	return s.GitHubClient.CommitCache(ctx, res.CacheID, info.Size())
}

func computeCacheKey(version, depHash string) string {
	platform := strings.ToLower(os.Getenv("RUNNER_OS"))
	if platform == "" {
		platform = runtime.GOOS
	}
	arch := strings.ToLower(os.Getenv("RUNNER_ARCH"))
	if arch == "" {
		arch = runtime.GOARCH
	}
	linuxVersion := ""
	if platform == "linux" {
		linuxVersion = os.Getenv("ImageOS") + "-"
	}
	return fmt.Sprintf("setup-go-%s-%s-%sgo-%s-%s", platform, arch, linuxVersion, version, depHash)
}

func dependencyFileHash(depPath string) string {
	workspace := os.Getenv("GITHUB_WORKSPACE")
	p := depPath
	if p == "" {
		p = filepath.Join(workspace, "go.mod")
	}
	if p != "" && !filepath.IsAbs(p) && workspace != "" {
		p = filepath.Join(workspace, p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
