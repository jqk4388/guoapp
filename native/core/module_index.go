package core

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	moduleIndexFormat  = 1
	moduleIndexBytes   = 1 << 20
	moduleIndexTimeout = 8 * time.Second
	moduleFetchTimeout = 12 * time.Second
)

var (
	moduleIndexAddresses = []string{
		"https://cdn.jsdelivr.net/gh/jqk4388/guoapp-modules-private@main/index.json",
		"https://fastly.jsdelivr.net/gh/jqk4388/guoapp-modules-private@main/index.json",
		"https://raw.githubusercontent.com/jqk4388/guoapp-modules-private/main/index.json",
	}
	moduleIndexCommunityAddresses = []string{
		"https://cdn.jsdelivr.net/gh/guoapp-modules/index@main/index.json",
		"https://raw.githubusercontent.com/guoapp-modules/index/main/index.json",
	}
	moduleIndexRepos = []moduleIndexRepoSource{
		{Repo: "jqk4388/guoapp-modules-private", Ref: "main", Path: "index.json"},
	}
	moduleFileAddresses = []string{
		"https://cdn.jsdelivr.net/gh/%s@%s/%s",
		"https://fastly.jsdelivr.net/gh/%s@%s/%s",
		"https://raw.githubusercontent.com/%s/%s/%s",
	}
	moduleIndexRepo = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)
	moduleIndexTag  = regexp.MustCompile(`^(?:[a-z0-9][a-z0-9_-]{0,31}-)?v\d+\.\d+\.\d+$`)
	moduleIndexHash = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type moduleIndexRepoSource struct {
	Repo string
	Ref  string
	Path string
}

type moduleIndexEntry struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	API         int               `json:"api"`
	Adult       bool              `json:"adult"`
	Repo        string            `json:"repo"`
	Tag         string            `json:"tag"`
	Path        string            `json:"path"`
	SHA256      map[string]string `json:"sha256"`
	Bundled     bool              `json:"bundled,omitempty"`
	repository  bool
}

func (entry moduleIndexEntry) usable() bool {
	return entry.API == moduleAPIVersion && moduleIDPattern.MatchString(entry.ID) && strings.TrimSpace(entry.Name) != "" &&
		moduleIndexRepo.MatchString(entry.Repo) && moduleIndexTag.MatchString(entry.Tag) &&
		moduleIndexHash.MatchString(entry.SHA256[moduleManifest]) && moduleIndexHash.MatchString(entry.SHA256[moduleEntry])
}

func parseModuleIndex(raw []byte) ([]moduleIndexEntry, error) {
	var index struct {
		Format  int                `json:"format"`
		Modules []moduleIndexEntry `json:"modules"`
	}
	if err := json.Unmarshal(raw, &index); err != nil {
		return nil, errors.New("索引格式无效")
	}
	if index.Format != moduleIndexFormat {
		return nil, fmt.Errorf("索引格式为第 %d 版，请更新应用", index.Format)
	}
	entries := []moduleIndexEntry{}
	for _, entry := range index.Modules {
		if entry.usable() {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (engine *nativeEngine) moduleIndex(ctx context.Context) ([]moduleIndexEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, moduleIndexTimeout)
	defer cancel()
	type fetched struct {
		entries []moduleIndexEntry
		err     error
	}
	addresses := make([]string, 0, len(moduleIndexAddresses)+len(moduleIndexCommunityAddresses))
	addresses = append(addresses, moduleIndexAddresses...)
	addresses = append(addresses, moduleIndexCommunityAddresses...)
	results := make([]fetched, len(addresses)+len(moduleIndexRepos))
	var wait sync.WaitGroup
	for index, address := range addresses {
		wait.Add(1)
		go func(index int, address string) {
			defer wait.Done()
			list, err := engine.fetchIndexList(ctx, address)
			results[index] = fetched{entries: list, err: err}
		}(index, address)
	}
	for offset, source := range moduleIndexRepos {
		index := len(addresses) + offset
		wait.Add(1)
		go func(index int, source moduleIndexRepoSource) {
			defer wait.Done()
			list, err := engine.fetchRepositoryIndex(ctx, source)
			results[index] = fetched{entries: list, err: err}
		}(index, source)
	}
	wait.Wait()

	entries := []moduleIndexEntry{}
	positions := map[string]int{}
	collect := func(list []moduleIndexEntry) {
		for _, entry := range list {
			position, found := positions[entry.ID]
			if !found {
				positions[entry.ID] = len(entries)
				entries = append(entries, entry)
				continue
			}
			if newerModuleVersion(entry.Version, entries[position].Version) {
				entries[position] = entry
			}
		}
	}
	var failures []string
	for _, result := range results {
		if result.err != nil {
			failures = append(failures, result.err.Error())
			continue
		}
		collect(result.entries)
	}
	if len(entries) == 0 {
		if len(failures) == 0 {
			failures = append(failures, "没有可用的模块源")
		}
		return nil, fmt.Errorf("读取模块索引失败：%s", strings.Join(failures, "；"))
	}
	return entries, nil
}

func (engine *nativeEngine) fetchIndexList(ctx context.Context, address string) ([]moduleIndexEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, moduleIndexTimeout)
	defer cancel()
	raw, err := engine.fetchModuleResource(ctx, address, moduleIndexBytes)
	if err != nil {
		return nil, err
	}
	list, err := parseModuleIndex(raw)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errors.New("索引里没有可安装的模块")
	}
	return list, nil
}

func (engine *nativeEngine) fetchRepositoryIndex(ctx context.Context, source moduleIndexRepoSource) ([]moduleIndexEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, moduleFetchTimeout)
	defer cancel()
	raw, err := engine.fetchRepositoryFile(ctx, source.Repo, source.Ref, source.Path, moduleIndexBytes)
	if err != nil {
		return nil, fmt.Errorf("%s：%w", source.Repo, err)
	}
	list, err := parseModuleIndex(raw)
	if err != nil {
		return nil, fmt.Errorf("%s：%w", source.Repo, err)
	}
	for index := range list {
		list[index].repository = true
	}
	return list, nil
}

func (engine *nativeEngine) moduleStore(ctx context.Context) (map[string]any, error) {
	entries, err := engine.moduleIndex(ctx)
	bundled := bundledIndexEntries()
	if err != nil && len(bundled) == 0 {
		return nil, err
	}
	items := append(bundled, entries...)
	result := map[string]any{"items": items}
	if err != nil {
		result["warning"] = err.Error()
	}
	return result, nil
}

func (engine *nativeEngine) indexedModulePackage(ctx context.Context, id string, bundled bool) ([]byte, error) {
	if bundled {
		modules, _ := readBundledModules()
		if modules[id].raw == nil {
			return nil, fmt.Errorf("安装包没有内置 %s", id)
		}
		return zipModuleFiles(modules[id].raw, modules[id].code)
	}
	entries, err := engine.moduleIndex(ctx)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.ID != id {
			continue
		}
		manifest, err := engine.fetchIndexedFile(ctx, entry, moduleManifest, moduleManifestBytes)
		if err != nil {
			return nil, err
		}
		code, err := engine.fetchIndexedFile(ctx, entry, moduleEntry, moduleEntryBytes)
		if err != nil {
			return nil, err
		}
		return zipModuleFiles(manifest, code)
	}
	return nil, fmt.Errorf("模块索引里没有 %s", id)
}

func (engine *nativeEngine) fetchIndexedFile(ctx context.Context, entry moduleIndexEntry, name string, limit int) ([]byte, error) {
	path := name
	if prefix := strings.Trim(strings.TrimSpace(entry.Path), "/"); prefix != "" {
		path = prefix + "/" + name
	}
	if entry.repository {
		ctx, cancel := context.WithTimeout(ctx, moduleFetchTimeout)
		defer cancel()
		data, err := engine.fetchRepositoryFile(ctx, entry.Repo, entry.Tag, path, limit)
		if err != nil {
			return nil, fmt.Errorf("下载 %s 的 %s 失败：%s", entry.Name, name, err.Error())
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != entry.SHA256[name] {
			return nil, fmt.Errorf("下载 %s 的 %s 失败：文件校验不符", entry.Name, name)
		}
		return data, nil
	}
	var failures []string
	for _, pattern := range moduleFileAddresses {
		data, err := engine.fetchModuleResource(ctx, fmt.Sprintf(pattern, entry.Repo, entry.Tag, path), limit)
		if err == nil {
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) == entry.SHA256[name] {
				return data, nil
			}
			err = errors.New("文件校验不符")
		}
		failures = append(failures, err.Error())
	}
	return nil, fmt.Errorf("下载 %s 的 %s 失败：%s", entry.Name, name, strings.Join(failures, "；"))
}

func (engine *nativeEngine) fetchModuleResource(ctx context.Context, address string, limit int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, moduleFetchTimeout)
	defer cancel()
	return engine.fetchLimited(ctx, address, limit)
}

func (engine *nativeEngine) moduleToken() string {
	engine.settingsMu.Lock()
	defer engine.settingsMu.Unlock()
	return strings.TrimSpace(engine.settings.ModuleToken)
}

func (engine *nativeEngine) fetchRepositoryFile(ctx context.Context, repo, ref, path string, limit int) ([]byte, error) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	address := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s?ref=%s", repo, strings.Join(segments, "/"), url.QueryEscape(ref))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github.raw+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "guoapp")
	token := engine.moduleToken()
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := engine.downloader.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		if token == "" {
			return nil, fmt.Errorf("模块源拒绝访问（HTTP %d）", response.StatusCode)
		}
		return nil, fmt.Errorf("模块源拒绝访问（HTTP %d），请检查访问令牌", response.StatusCode)
	case http.StatusNotFound:
		return nil, errors.New("模块源里没有该文件")
	default:
		return nil, fmt.Errorf("HTTP %s", strconv.Itoa(response.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > limit {
		if limit >= 1<<20 {
			return nil, fmt.Errorf("文件超过 %d MB", limit>>20)
		}
		return nil, fmt.Errorf("文件超过 %d KB", limit>>10)
	}
	return raw, nil
}

func zipModuleFiles(manifest, code []byte) ([]byte, error) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, file := range []struct {
		name string
		data []byte
	}{{moduleManifest, manifest}, {moduleEntry, code}} {
		writer, err := archive.Create(file.name)
		if err != nil {
			return nil, err
		}
		if _, err := writer.Write(file.data); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
