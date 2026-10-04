// Command hello-scan-source is a minimal scan_source.v1 plugin for Silo
// Autoscan. Another tool appends changed paths to a change-log file, one per
// line; each poll returns the lines added since the previous poll.
//
// The marker is the number of lines already consumed. The host stores it
// verbatim and sends it back on the next poll, so the plugin keeps no state of
// its own.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	publicmanifest "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
	sdkruntime "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"
)

//go:embed manifest.json
var manifestJSON []byte

// changesFileKey is the source_config key declared in manifest.json's
// config_form. The operator fills it in when adding the source.
const changesFileKey = "changes_file"

type runtimeServer struct {
	pluginv1.UnimplementedRuntimeServer
	manifest *pluginv1.PluginManifest
}

func (s *runtimeServer) GetManifest(context.Context, *pluginv1.GetManifestRequest) (*pluginv1.GetManifestResponse, error) {
	return &pluginv1.GetManifestResponse{Manifest: s.manifest}, nil
}

type scanSourceServer struct {
	pluginv1.UnimplementedScanSourceServer
}

func (scanSourceServer) PollChanges(_ context.Context, req *pluginv1.PollChangesRequest) (*pluginv1.PollChangesResponse, error) {
	path := strings.TrimSpace(req.GetSourceConfig()[changesFileKey])
	if path == "" {
		// The host records this message on the source and in the Autoscan
		// activity log, and keeps the marker, so write it for the operator.
		return nil, fmt.Errorf("%s is not configured", changesFileKey)
	}
	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}

	// Always return a non-empty marker. An empty next_marker is stored as "no
	// marker", which makes the next poll look like a first poll again.
	resp := &pluginv1.PollChangesResponse{NextMarker: strconv.Itoa(len(lines))}

	// An empty marker means this is the first poll for the source: start from
	// now and do not replay what the file already holds.
	if req.GetMarker() == "" {
		return resp, nil
	}
	consumed, err := strconv.Atoi(req.GetMarker())
	if err != nil || consumed < 0 || consumed > len(lines) {
		// The file was truncated or rotated, or the marker is not ours.
		// Resynchronize to the end instead of replaying the whole file.
		return resp, nil
	}

	for _, line := range lines[consumed:] {
		resp.Changes = append(resp.Changes, changeFor(line))
	}
	return resp, nil
}

// changeFor turns one change-log line into a structured change. A trailing
// slash marks a directory, which is reported as a subtree. Anything else is
// reported as a file; the host widens a video file to a scan of its directory
// and scans an existing directory as a subtree.
func changeFor(line string) *pluginv1.ScanSourceChange {
	if strings.HasSuffix(line, "/") {
		return &pluginv1.ScanSourceChange{
			SourcePath: strings.TrimRight(line, "/"),
			Scope:      pluginv1.ScanSourceChangeScope_SCAN_SOURCE_CHANGE_SCOPE_SUBTREE,
		}
	}
	return &pluginv1.ScanSourceChange{
		SourcePath: line,
		Scope:      pluginv1.ScanSourceChangeScope_SCAN_SOURCE_CHANGE_SCOPE_FILE,
	}
}

// readLines returns the non-blank lines of the change log. A missing file is
// an empty log, so a source can be created before the other tool first writes.
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open change log: %w", err)
	}
	defer func() { _ = f.Close() }() // read-only; a close error cannot lose data

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read change log: %w", err)
	}
	return lines, nil
}

func main() {
	manifest, err := loadManifest()
	if err != nil {
		panic(err)
	}

	sdkruntime.Serve(sdkruntime.ServeConfig{
		Servers: sdkruntime.CapabilityServers{
			Runtime:    &runtimeServer{manifest: manifest},
			ScanSource: scanSourceServer{},
		},
	})
}

func loadManifest() (*pluginv1.PluginManifest, error) {
	manifest, err := publicmanifest.Load(manifestJSON)
	if err != nil {
		return nil, fmt.Errorf("load embedded manifest: %w", err)
	}

	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	binaryData, err := os.ReadFile(executablePath)
	if err != nil {
		return nil, fmt.Errorf("read executable %q: %w", executablePath, err)
	}
	checksum := sha256.Sum256(binaryData)
	manifest.Checksum = hex.EncodeToString(checksum[:])
	if len(manifest.GetSupportedPlatforms()) == 0 {
		manifest.SupportedPlatforms = []*pluginv1.SupportedPlatform{
			{Os: goruntime.GOOS, Arch: goruntime.GOARCH},
		}
	}

	return manifest, nil
}
