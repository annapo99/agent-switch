package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultRepo     = "annapo99/agent-switch"
	defaultCacheTTL = 12 * time.Hour
)

type Config struct {
	Home            string
	CurrentVersion  string
	ExecutablePath  string
	Repo            string
	LatestURL       string
	DownloadBaseURL string
	OS              string
	Arch            string
	Client          *http.Client
	Now             func() time.Time
	CacheTTL        time.Duration
}

type Info struct {
	Version   string
	Available bool
}

type Result struct {
	Version string
	Updated bool
	Path    string
}

type releasePayload struct {
	TagName string `json:"tag_name"`
}

type cacheFile struct {
	Version   string    `json:"version"`
	CheckedAt time.Time `json:"checked_at"`
}

func IsNewer(latest, current string) bool {
	latestParts := versionParts(latest)
	currentParts := versionParts(current)
	count := len(latestParts)
	if len(currentParts) > count {
		count = len(currentParts)
	}
	for i := 0; i < count; i++ {
		latestPart := partAt(latestParts, i)
		currentPart := partAt(currentParts, i)
		if latestPart > currentPart {
			return true
		}
		if latestPart < currentPart {
			return false
		}
	}
	return false
}

func CheckLatest(ctx context.Context, cfg Config) (Info, error) {
	cfg = withDefaults(cfg)
	if isDevelopmentVersion(cfg.CurrentVersion) {
		return Info{}, nil
	}
	if cached, ok := readFreshCache(cfg); ok {
		return infoForVersion(cached.Version, cfg.CurrentVersion), nil
	}
	version, err := fetchLatestVersion(ctx, cfg)
	if err != nil {
		return Info{}, err
	}
	_ = writeCache(cfg, version)
	return infoForVersion(version, cfg.CurrentVersion), nil
}

func isDevelopmentVersion(version string) bool {
	version = strings.TrimSpace(strings.ToLower(version))
	return version == "" || version == "dev" || version == "(devel)"
}

func Update(ctx context.Context, cfg Config) (Result, error) {
	cfg = withDefaults(cfg)
	version, err := fetchLatestVersion(ctx, cfg)
	if err != nil {
		return Result{}, err
	}
	if !IsNewer(version, cfg.CurrentVersion) {
		return Result{Version: version, Updated: false, Path: cfg.ExecutablePath}, nil
	}
	asset := fmt.Sprintf("ags_%s_%s.tar.gz", cfg.OS, cfg.Arch)
	baseURL := cfg.DownloadBaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://github.com/%s/releases/download/%s", cfg.Repo, version)
	}
	baseURL = strings.TrimRight(baseURL, "/")
	archive, err := download(ctx, cfg, baseURL+"/"+asset)
	if err != nil {
		return Result{}, err
	}
	checksums, err := download(ctx, cfg, baseURL+"/checksums.txt")
	if err != nil {
		return Result{}, err
	}
	if err := verifyChecksum(asset, archive, string(checksums)); err != nil {
		return Result{}, err
	}
	binary, err := extractAGS(archive)
	if err != nil {
		return Result{}, err
	}
	if err := replaceExecutable(cfg.ExecutablePath, binary); err != nil {
		return Result{}, err
	}
	_ = writeCache(cfg, version)
	return Result{Version: version, Updated: true, Path: cfg.ExecutablePath}, nil
}

func withDefaults(cfg Config) Config {
	if cfg.Home == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cfg.Home = home
		}
	}
	if cfg.Repo == "" {
		cfg.Repo = DefaultRepo
	}
	if cfg.LatestURL == "" {
		cfg.LatestURL = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", cfg.Repo)
	}
	if cfg.OS == "" {
		cfg.OS = runtime.GOOS
	}
	if cfg.Arch == "" {
		cfg.Arch = normalizeArch(runtime.GOARCH)
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 2 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = defaultCacheTTL
	}
	if cfg.ExecutablePath == "" {
		if path, err := os.Executable(); err == nil {
			cfg.ExecutablePath = path
		}
	}
	return cfg
}

func infoForVersion(version, current string) Info {
	return Info{Version: version, Available: IsNewer(version, current)}
}

func fetchLatestVersion(ctx context.Context, cfg Config) (string, error) {
	body, err := download(ctx, cfg, cfg.LatestURL)
	if err != nil {
		return "", err
	}
	var payload releasePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.TagName) == "" {
		return "", fmt.Errorf("latest release has no tag_name")
	}
	return strings.TrimSpace(payload.TagName), nil
}

func download(ctx context.Context, cfg Config, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "agent-switch")
	resp, err := cfg.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func verifyChecksum(asset string, data []byte, checksums string) error {
	var expected string
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[len(fields)-1] == asset {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksum not found for %s", asset)
	}
	actual := sha256.Sum256(data)
	if !strings.EqualFold(expected, fmt.Sprintf("%x", actual)) {
		return fmt.Errorf("checksum mismatch for %s", asset)
	}
	return nil
}

func extractAGS(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == "ags" {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("release archive does not contain ags")
}

func replaceExecutable(path string, binary []byte) error {
	if path == "" {
		return fmt.Errorf("cannot locate current ags executable")
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ags-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(binary); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func readFreshCache(cfg Config) (cacheFile, bool) {
	data, err := os.ReadFile(cachePath(cfg.Home))
	if err != nil {
		return cacheFile{}, false
	}
	var cache cacheFile
	if err := json.Unmarshal(data, &cache); err != nil || cache.Version == "" {
		return cacheFile{}, false
	}
	return cache, cfg.Now().Sub(cache.CheckedAt) < cfg.CacheTTL
}

func writeCache(cfg Config, version string) error {
	path := cachePath(cfg.Home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cacheFile{Version: version, CheckedAt: cfg.Now()}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func cachePath(home string) string {
	return filepath.Join(home, ".agent-switch", "update-cache.json")
}

func versionParts(version string) []int {
	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	if before, _, ok := strings.Cut(version, "-"); ok {
		version = before
	}
	var parts []int
	for _, part := range strings.Split(version, ".") {
		value, err := strconv.Atoi(part)
		if err != nil {
			value = 0
		}
		parts = append(parts, value)
	}
	return parts
}

func partAt(parts []int, index int) int {
	if index >= len(parts) {
		return 0
	}
	return parts[index]
}

func normalizeArch(arch string) string {
	switch arch {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return arch
	}
}
