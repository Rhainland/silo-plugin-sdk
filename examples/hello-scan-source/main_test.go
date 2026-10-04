package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	publicmanifest "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
)

const (
	scopeFile    = pluginv1.ScanSourceChangeScope_SCAN_SOURCE_CHANGE_SCOPE_FILE
	scopeSubtree = pluginv1.ScanSourceChangeScope_SCAN_SOURCE_CHANGE_SCOPE_SUBTREE
)

func poll(t *testing.T, changesFile, marker string) *pluginv1.PollChangesResponse {
	t.Helper()
	resp, err := scanSourceServer{}.PollChanges(context.Background(), &pluginv1.PollChangesRequest{
		CapabilityId: "changelog",
		Marker:       marker,
		SourceConfig: map[string]string{changesFileKey: changesFile},
	})
	if err != nil {
		t.Fatalf("PollChanges(marker=%q) error = %v", marker, err)
	}
	return resp
}

func writeLog(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPollChangesFirstPollStartsFromNow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "changes.log")
	writeLog(t, path, "/media/movies/Old (2001)/Old.mkv", "/media/movies/Older (1999)/Older.mkv")

	resp := poll(t, path, "")
	if len(resp.GetChanges()) != 0 {
		t.Fatalf("first poll replayed %d changes, want none", len(resp.GetChanges()))
	}
	if got := resp.GetNextMarker(); got != "2" {
		t.Fatalf("next_marker = %q, want %q", got, "2")
	}
}

func TestPollChangesReturnsLinesSinceMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "changes.log")
	writeLog(t, path,
		"/media/movies/Old (2001)/Old.mkv",
		"",
		"/media/movies/New (2024)/New.mkv",
		"/media/tv/Gone Show/",
	)

	resp := poll(t, path, "1")
	want := []*pluginv1.ScanSourceChange{
		{SourcePath: "/media/movies/New (2024)/New.mkv", Scope: scopeFile},
		{SourcePath: "/media/tv/Gone Show", Scope: scopeSubtree},
	}
	got := resp.GetChanges()
	if len(got) != len(want) {
		t.Fatalf("got %d changes, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].GetSourcePath() != want[i].GetSourcePath() || got[i].GetScope() != want[i].GetScope() {
			t.Fatalf("change %d = {%q %v}, want {%q %v}", i,
				got[i].GetSourcePath(), got[i].GetScope(), want[i].GetSourcePath(), want[i].GetScope())
		}
	}
	if got := resp.GetNextMarker(); got != "3" {
		t.Fatalf("next_marker = %q, want %q", got, "3")
	}

	// Polling again with the returned marker yields nothing new and keeps the
	// position, so a held marker re-reads the same window.
	again := poll(t, path, resp.GetNextMarker())
	if len(again.GetChanges()) != 0 || again.GetNextMarker() != "3" {
		t.Fatalf("repeat poll = %d changes, marker %q; want 0 changes, marker %q",
			len(again.GetChanges()), again.GetNextMarker(), "3")
	}
}

func TestPollChangesResyncsAfterTruncationOrForeignMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "changes.log")
	writeLog(t, path, "/media/movies/A (2020)/A.mkv")

	for _, marker := range []string{"10", "-1", "not-a-number"} {
		resp := poll(t, path, marker)
		if len(resp.GetChanges()) != 0 {
			t.Fatalf("marker %q replayed %d changes, want none", marker, len(resp.GetChanges()))
		}
		if got := resp.GetNextMarker(); got != "1" {
			t.Fatalf("marker %q: next_marker = %q, want %q", marker, got, "1")
		}
	}
}

func TestPollChangesMissingFileIsEmptyLog(t *testing.T) {
	resp := poll(t, filepath.Join(t.TempDir(), "absent.log"), "")
	if len(resp.GetChanges()) != 0 {
		t.Fatalf("got %d changes, want none", len(resp.GetChanges()))
	}
	if got := resp.GetNextMarker(); got != "0" {
		t.Fatalf("next_marker = %q, want a non-empty %q", got, "0")
	}
}

func TestPollChangesRequiresChangesFile(t *testing.T) {
	_, err := scanSourceServer{}.PollChanges(context.Background(), &pluginv1.PollChangesRequest{})
	if err == nil || !strings.Contains(err.Error(), changesFileKey) {
		t.Fatalf("PollChanges without %s = %v, want an error naming the key", changesFileKey, err)
	}
}

func TestManifestDeclaresScanSource(t *testing.T) {
	manifest, err := publicmanifest.Load(manifestJSON)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	caps := manifest.GetCapabilities()
	if len(caps) != 1 || caps[0].GetType() != "scan_source.v1" {
		t.Fatalf("capabilities = %v, want one scan_source.v1", caps)
	}
	block, ok := caps[0].GetMetadata().AsMap()["scan_source"].(map[string]any)
	if !ok {
		t.Fatal("capability metadata has no scan_source descriptor")
	}
	form, _ := block["config_form"].(map[string]any)
	fields, _ := form["fields"].([]any)
	if len(fields) != 1 || fields[0].(map[string]any)["key"] != changesFileKey {
		t.Fatalf("config_form fields = %v, want one %q field", fields, changesFileKey)
	}
}
