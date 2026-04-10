package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	skillConcurrency = 4
	fileConcurrency  = 8
)

type RemoteIndex struct {
	Skills []RemoteIndexSkill `json:"skills"`
}

type RemoteIndexSkill struct {
	Name  string   `json:"name"`
	Files []string `json:"files"`
}

type RemoteSkillLoader struct {
	cacheDir string
	client   *http.Client
}

func NewRemoteSkillLoader(cacheDir string) *RemoteSkillLoader {
	return &RemoteSkillLoader{
		cacheDir: cacheDir,
		client:   &http.Client{Timeout: 30 * 1000},
	}
}

func (r *RemoteSkillLoader) Pull(ctx context.Context, baseURL string) ([]string, error) {
	base := baseURL
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}

	indexURL, err := url.Parse(base + "index.json")
	if err != nil {
		return nil, fmt.Errorf("invalid skill URL: %w", err)
	}

	resp, err := r.client.Get(indexURL.String())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch index: status %d", resp.StatusCode)
	}

	var idx RemoteIndex
	if err := json.NewDecoder(resp.Body).Decode(&idx); err != nil {
		return nil, fmt.Errorf("failed to parse index: %w", err)
	}

	validSkills := make([]RemoteIndexSkill, 0, len(idx.Skills))
	for _, skill := range idx.Skills {
		hasSKILLMD := false
		for _, f := range skill.Files {
			if f == "SKILL.md" {
				hasSKILLMD = true
				break
			}
		}
		if !hasSKILLMD {
			continue
		}
		validSkills = append(validSkills, skill)
	}

	type downloadResult struct {
		path string
		ok   bool
	}

	results := make([]downloadResult, len(validSkills))
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, skillConcurrency)

	for i, skill := range validSkills {
		wg.Add(1)
		go func(i int, skill RemoteIndexSkill) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			skillRoot := filepath.Join(r.cacheDir, skill.Name)
			if err := os.MkdirAll(skillRoot, 0o755); err != nil {
				results[i] = downloadResult{ok: false}
				return
			}

			fileSem := make(chan struct{}, fileConcurrency)
			var fileWg sync.WaitGroup

			for _, file := range skill.Files {
				fileWg.Add(1)
				go func(file string) {
					defer fileWg.Done()
					fileSem <- struct{}{}
					defer func() { <-fileSem }()

					fileURL := base + skill.Name + "/" + file
					destPath := filepath.Join(skillRoot, file)

					if _, err := os.Stat(destPath); err == nil {
						return
					}

					resp, err := r.client.Get(fileURL)
					if err != nil {
						return
					}
					defer resp.Body.Close()

					if resp.StatusCode != http.StatusOK {
						return
					}

					data, err := io.ReadAll(resp.Body)
					if err != nil {
						return
					}

					os.WriteFile(destPath, data, 0o644)
				}(file)
			}

			fileWg.Wait()

			skillMDPath := filepath.Join(skillRoot, "SKILL.md")
			if _, err := os.Stat(skillMDPath); err == nil {
				results[i] = downloadResult{path: skillRoot, ok: true}
			} else {
				results[i] = downloadResult{ok: false}
			}
		}(i, skill)
	}

	wg.Wait()

	var paths []string
	for _, result := range results {
		if result.ok && result.path != "" {
			paths = append(paths, result.path)
		}
	}

	return paths, nil
}

func (r *RemoteSkillLoader) PullToLoader(ctx context.Context, baseURL string, loader *Loader) ([]string, error) {
	paths, err := r.Pull(ctx, baseURL)
	if err != nil {
		return nil, err
	}

	loader.AddPaths(paths)
	if err := loader.LoadCustom(ctx); err != nil {
		return nil, err
	}

	return paths, nil
}
