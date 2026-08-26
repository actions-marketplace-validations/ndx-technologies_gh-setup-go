package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
)

func main() {
	var (
		version   string
		cache     bool
		depPath   string
		saveCache bool
		key       string
	)
	flag.StringVar(&version, "version", "", "Go version to use")
	flag.BoolVar(&cache, "cache", true, "restore the Go module cache")
	flag.BoolVar(&saveCache, "save-cache", false, "save the Go module cache (normal step)")
	flag.StringVar(&depPath, "cache-dependency-path", "", "dependency file used for the cache key")
	flag.StringVar(&key, "key", "", "cache key (for -save-cache)")
	flag.Parse()

	cacheBaseURL := os.Getenv("ACTIONS_CACHE_URL")
	if cacheBaseURL == "" {
		cacheBaseURL = os.Getenv("ACTIONS_RESULTS_URL")
	}

	httpClient := http.DefaultClient
	s := CacheService{
		GitHubClient: GitHubClient{
			Config: GitHubClientConfig{
				BaseURL: cacheBaseURL,
			},
			Secrets: GitHubClientSecrets{Token: os.Getenv("ACTIONS_RUNTIME_TOKEN")},
			Client:  httpClient,
		},
		HTTPClient: httpClient,
	}

	var cachePaths []string
	if p := os.Getenv("GOMODCACHE"); p != "" {
		cachePaths = append(cachePaths, p)
	}
	if p := os.Getenv("GOCACHE"); p != "" {
		cachePaths = append(cachePaths, p)
	}

	ctx := context.Background()

	if saveCache {
		if key != "" {
			if len(cachePaths) == 0 {
				fmt.Fprintln(os.Stderr, "::warning::cache save: no cache paths found")
			}
			if err := s.SaveCache(ctx, cachePaths, key); err != nil {
				fmt.Fprintf(os.Stderr, "::warning::cache save: %v\n", err)
				return
			}
			fmt.Printf("cache saved with the key: %s\n", key)
		}
		return
	}

	if version == "" {
		fmt.Fprintln(os.Stderr, "::error::go-version is required (full version like 1.27.0)")
		os.Exit(1)
		return
	}

	ghout := make(map[string]string)

	if cache && cacheBaseURL != "" && len(cachePaths) > 0 {
		key := computeCacheKey(version, dependencyFileHash(depPath))

		matched, err := s.RestoreCache(ctx, cachePaths, key)
		if err != nil {
			fmt.Printf("::warning::restore cache failed: %s\n", err)
		} else {
			if matched != "" {
				fmt.Printf("cache restored from key: %s\n", matched)
			} else {
				fmt.Printf("cache is not found\n")
				ghout["key"] = key
			}
		}
	}

	if len(ghout) > 0 {
		outFile := os.Getenv("GITHUB_OUTPUT")
		f, err := os.OpenFile(outFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()

		for k, v := range ghout {
			fmt.Fprintf(f, "%s=%v\n", k, v)
		}
	}
}
