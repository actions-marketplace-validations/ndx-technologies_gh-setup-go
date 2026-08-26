package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

type GitHubClientSecrets struct {
	Token string
}

type GitHubClientConfig struct {
	BaseURL string
}

type GitHubClient struct {
	Config  GitHubClientConfig
	Secrets GitHubClientSecrets
	Client  *http.Client
}

func (s GitHubClient) do(ctx context.Context, method, u string, body []byte, hdr map[string]string) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	if s.Secrets.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Secrets.Token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return s.Client.Do(req)
}

const (
	cacheAPIVersion = "application/json;api-version=6.0-preview.1"
	chunkSize       = 32 * 1024 * 1024 // cache upload chunk size
)

type CacheEntryResponse struct {
	ArchiveLocation string `json:"archiveLocation"`
}

type ErrHTTP struct{ StatusCode int }

func (s *ErrHTTP) Error() string { return "http error: " + strconv.Itoa(s.StatusCode) }

func (s GitHubClient) GetCacheEntry(ctx context.Context, key, version string) (*CacheEntryResponse, error) {
	u := s.Config.BaseURL + "_apis/artifactcache/cache?keys=" + url.QueryEscape(key) + "&version=" + version
	resp, err := s.do(ctx, "GET", u, nil, map[string]string{"Accept": cacheAPIVersion})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &ErrHTTP{StatusCode: resp.StatusCode}
	}

	var v CacheEntryResponse
	if err := json.UnmarshalRead(resp.Body, &v); err != nil {
		return nil, err
	}
	if v.ArchiveLocation == "" {
		return nil, fmt.Errorf("cache entry has no archive location")
	}
	return &v, nil
}

type CacheID int64

type ReserveCacheRequest struct {
	Key     string `json:"key"`
	Version string `json:"version"`
}

type ReserveCacheResponse struct {
	CacheID CacheID `json:"cacheId"`
}

func (s GitHubClient) ReserveCache(ctx context.Context, key, version string) (*ReserveCacheResponse, error) {
	req := ReserveCacheRequest{
		Key:     key,
		Version: version,
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := s.do(ctx, "POST", s.Config.BaseURL+"_apis/artifactcache/caches", reqBody, map[string]string{"Accept": cacheAPIVersion})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, &ErrHTTP{StatusCode: resp.StatusCode}
	}

	var v ReserveCacheResponse
	if err := json.UnmarshalRead(resp.Body, &v); err != nil {
		return nil, err
	}

	return &v, nil
}

func (s GitHubClient) UploadCacheChunk(ctx context.Context, id CacheID, offset int64, chunk []byte) error {
	u := fmt.Sprintf("%s_apis/artifactcache/caches/%d?offset=%d", s.Config.BaseURL, id, offset)
	hdr := map[string]string{
		"Accept":        cacheAPIVersion,
		"Content-Type":  "application/octet-stream",
		"Content-Range": fmt.Sprintf("bytes %d-%d/*", offset, offset+int64(len(chunk))-1),
	}
	resp, err := s.do(ctx, "PATCH", u, chunk, hdr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &ErrHTTP{StatusCode: resp.StatusCode}
	}

	return nil
}

func (s GitHubClient) UploadCache(ctx context.Context, id CacheID, r io.Reader) error {
	buf := make([]byte, chunkSize)
	var offset int64
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			if uerr := s.UploadCacheChunk(ctx, id, offset, buf[:n]); uerr != nil {
				return uerr
			}
			offset += int64(n)
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

type CommitCacheRequest struct {
	Size int64 `json:"size"`
}

func (s GitHubClient) CommitCache(ctx context.Context, id CacheID, size int64) error {
	req := CommitCacheRequest{
		Size: size,
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return err
	}

	u := fmt.Sprintf("%s_apis/artifactcache/caches/%d", s.Config.BaseURL, id)
	resp, err := s.do(ctx, "POST", u, reqBody, map[string]string{"Accept": cacheAPIVersion})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &ErrHTTP{StatusCode: resp.StatusCode}
	}

	return nil
}
