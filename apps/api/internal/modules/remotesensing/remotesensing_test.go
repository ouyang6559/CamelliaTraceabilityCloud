package remotesensing

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestMockProcessorPipeline verifies the deterministic mock: Start returns an
// id, Poll promotes every input to an output and reaches 100% in one poll.
func TestMockProcessorPipeline(t *testing.T) {
	p := &MockProcessor{}
	id, err := p.Start(context.Background(), "orthophoto", []JobInput{
		{FileID: "f1", Filename: "IMG_0001.JPG"},
		{FileID: "f2", Filename: "IMG_0002.JPG"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if id == "" {
		t.Fatal("empty external id")
	}
	state, err := p.Poll(context.Background(), "orthophoto", id, []JobInput{
		{FileID: "f1", Filename: "IMG_0001.JPG"},
		{FileID: "f2", Filename: "IMG_0002.JPG"},
	})
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if state.Status != "succeeded" || state.Progress != 100 {
		t.Fatalf("unexpected state: %+v", state)
	}
	if len(state.Outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(state.Outputs))
	}
	for i, out := range state.Outputs {
		if out.FileID == "" || out.Type != "orthophoto" {
			t.Errorf("output[%d] invalid: %+v", i, out)
		}
	}
}

// TestBuildStacItem verifies the OGC STAC 1.0.0 Item structure (specs/04.10):
// type/id/collection/properties.datetime/assets with href + links.
func TestBuildStacItem(t *testing.T) {
	item := buildStacItem("01ITEM", "orthophoto", "01JOB", "mock",
		JobOutput{FileID: "f1", Type: "orthophoto", Name: "ortho.tif"}, testNow())
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed struct {
		StacVersion string `json:"stac_version"`
		Type        string `json:"type"`
		ID          string `json:"id"`
		Collection  string `json:"collection"`
		Properties  struct {
			Datetime string `json:"datetime"`
		} `json:"properties"`
		Assets map[string]struct {
			Href  string   `json:"href"`
			Roles []string `json:"roles"`
		} `json:"assets"`
		Links []struct {
			Rel  string `json:"rel"`
			Href string `json:"href"`
		} `json:"links"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal item: %v", err)
	}
	if parsed.StacVersion != "1.0.0" || parsed.Type != "Feature" || parsed.ID != "01ITEM" {
		t.Errorf("stac core fields wrong: %s", raw)
	}
	if parsed.Collection != "orthophoto" {
		t.Errorf("collection wrong: %s", parsed.Collection)
	}
	if parsed.Properties.Datetime == "" {
		t.Error("properties.datetime missing")
	}
	ortho, ok := parsed.Assets["orthophoto"]
	if !ok || ortho.Href == "" {
		t.Fatalf("orthophoto asset missing: %v", parsed.Assets)
	}
	foundCollectionLink := false
	for _, l := range parsed.Links {
		if l.Rel == "collection" {
			foundCollectionLink = true
		}
	}
	if !foundCollectionLink {
		t.Error("collection link missing")
	}
	_ = time.Now
}

// TestNodeODMProcessorHonestFailures verifies the NodeODM client never
// fabricates results: Start always fails without a live instance and Poll
// rejects mock ids (specs/06.11).
func TestNodeODMProcessorHonestFailures(t *testing.T) {
	p := &NodeODMProcessor{BaseURL: "http://127.0.0.1:1"} // nothing listens here
	if _, err := p.Start(context.Background(), "orthophoto", nil); err == nil {
		t.Fatal("Start must fail (integration TODO) without a live NodeODM")
	}
	if _, err := p.Poll(context.Background(), "orthophoto", "mock-123", nil); err == nil {
		t.Fatal("Poll must reject mock task ids")
	}
}

func testNow() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) }
