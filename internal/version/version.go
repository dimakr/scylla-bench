package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

var (
	// Default version values; can be overridden via ldflags
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

type ComponentInfo struct {
	Version    string `json:"version"`
	CommitDate string `json:"commit_date"`
	CommitSHA  string `json:"commit_sha"`
}

type VersionInfo struct {
	ScyllaBench ComponentInfo `json:"scylla-bench"`
	Driver      ComponentInfo `json:"scylla-driver"`
}

type githubRelease struct {
	TagName   string    `json:"tag_name"`
	CreatedAt time.Time `json:"created_at"`
}

type githubTag struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

const (
	gocqlPackage  = "github.com/gocql/gocql"
	githubTimeout = 5 * time.Second
	userAgent     = "scylla-bench (github.com/scylladb/scylla-bench)"
)

var githubAPIBaseURL = "https://api.github.com"

// Performs an HTTP GET request to fetch release data in JSON format from GitHub
func getJSON(client *http.Client, url string, target interface{}) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("failed to decode JSON: %w", err)
	}
	return nil
}

// Retrieves the release date and commit SHA for a given scylladb repository and version from GitHub
func getReleaseInfo(repo, version string) (date, sha string, err error) {
	client := &http.Client{Timeout: githubTimeout}

	// Fetch releases
	releasesURL := fmt.Sprintf("%s/repos/scylladb/%s/releases", githubAPIBaseURL, repo)
	var releases []githubRelease
	if err := getJSON(client, releasesURL, &releases); err != nil {
		return "", "", fmt.Errorf("failed to fetch releases: %w", err)
	}

	// Find matching release
	cleanVersion := strings.TrimPrefix(version, "v")
	var releaseDate, tagName string
	for _, release := range releases {
		if strings.TrimPrefix(release.TagName, "v") == cleanVersion {
			releaseDate = release.CreatedAt.Format(time.RFC3339)
			tagName = release.TagName
			break
		}
	}
	if tagName == "" {
		return "", "", fmt.Errorf("release %s not found", version)
	}

	// Fetch tag info to get the commit SHA
	tagURL := fmt.Sprintf("%s/repos/scylladb/%s/git/refs/tags/%s", githubAPIBaseURL, repo, tagName)
	var tag githubTag
	if err := getJSON(client, tagURL, &tag); err != nil {
		return releaseDate, "", fmt.Errorf("failed to fetch tag info: %w", err)
	}

	return releaseDate, tag.Object.SHA, nil
}

// Retrieves scylla-bench version info from build info
func readMainBuildInfo() (ver, sha, buildDate string) {
	ver = version
	sha = commit
	buildDate = date
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if sha == "unknown" {
					sha = setting.Value
				}
			case "vcs.time":
				if buildDate == "unknown" {
					buildDate = setting.Value
				}
			}
		}
		if ver == "dev" {
			if info.Main.Version != "" {
				ver = info.Main.Version
			} else {
				ver = "(devel)"
			}
		}
	}
	return
}

// Retrieves scylla-gocql-driver version info from build info
func getDriverInfo() ComponentInfo {
	comp := ComponentInfo{
		Version:    "unknown",
		CommitSHA:  "unknown",
		CommitDate: "unknown",
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, d := range info.Deps {
			if d.Path == gocqlPackage {
				comp.Version = d.Version
				if d.Replace != nil {
					comp.Version = d.Replace.Version
					if dDate, dSHA, err := getReleaseInfo("gocql", comp.Version); err == nil {
						comp.CommitDate = dDate
						comp.CommitSHA = dSHA
					} else {
						comp.CommitSHA = "unknown"
						comp.CommitDate = "unknown"
					}
				} else {
					comp.CommitSHA = "upstream release"
					comp.CommitDate = "unknown"
				}
				break
			}
		}
	}
	return comp
}

// Returns the version info for scylla-bench and scylla-gocql-driver
func GetVersionInfo() VersionInfo {
	ver, sha, buildDate := readMainBuildInfo()
	driverInfo := getDriverInfo()
	return VersionInfo{
		ScyllaBench: ComponentInfo{
			Version:    ver,
			CommitDate: buildDate,
			CommitSHA:  sha,
		},
		Driver: driverInfo,
	}
}

// Returns a human-readable string with version info
func (v VersionInfo) FormatHuman() string {
	return fmt.Sprintf(`scylla-bench:
    version: %s
    commit sha: %s
    commit date: %s
scylla-gocql-driver:
    version: %s
    commit sha: %s
    commit date: %s`,
		v.ScyllaBench.Version,
		v.ScyllaBench.CommitSHA,
		v.ScyllaBench.CommitDate,
		v.Driver.Version,
		v.Driver.CommitSHA,
		v.Driver.CommitDate,
	)
}

// Returns a JSON-formatted string with version info
func (v VersionInfo) FormatJSON() (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal version info to JSON: %w", err)
	}
	return string(data), nil
}
