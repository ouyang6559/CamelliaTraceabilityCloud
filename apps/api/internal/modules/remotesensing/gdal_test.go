package remotesensing

import (
	"strings"
	"testing"
)

// TestBuildGdalTranslateArgs pins the COG conversion command per job type
// (docs/adr-011): orthophoto → deflate COG + overviews; dem → Float32;
// ndvi → honest TODO (band math needs multispectral sources).
func TestBuildGdalTranslateArgs(t *testing.T) {
	args, err := buildGdalTranslateArgs("orthophoto", "in.tif", "out.cog.tif")
	if err != nil {
		t.Fatalf("orthophoto: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-of COG") || !strings.Contains(joined, "OVERVIEWS=AUTO") {
		t.Errorf("orthophoto args missing COG/overviews: %s", joined)
	}
	if args[len(args)-2] != "in.tif" || args[len(args)-1] != "out.cog.tif" {
		t.Errorf("in/out paths wrong: %v", args)
	}

	args, err = buildGdalTranslateArgs("dem", "in.tif", "out.cog.tif")
	if err != nil {
		t.Fatalf("dem: %v", err)
	}
	if !strings.Contains(strings.Join(args, " "), "-ot Float32") {
		t.Errorf("dem args missing Float32: %v", args)
	}

	if _, err := buildGdalTranslateArgs("ndvi", "in.tif", "out.tif"); err == nil {
		t.Error("ndvi must fail honestly (band math TODO)")
	}
	if _, err := buildGdalTranslateArgs("unknown", "in", "out"); err == nil {
		t.Error("unknown job_type must fail")
	}
}

// TestGDALProcessorHonestStart verifies the processor refuses to pretend when
// gdal_translate is unavailable (specs/06.11) and ndvi jobs are rejected.
func TestGDALProcessorHonestStart(t *testing.T) {
	p := &GDALProcessor{bin: "/nonexistent/gdal_translate", state: map[string]*gdalState{}}
	if _, err := p.Start(t.Context(), "orthophoto",
		[]JobInput{{FileID: "f1", Filename: "a.tif"}}); err == nil {
		t.Fatal("Start must fail when gdal_translate is missing")
	} else if !strings.Contains(err.Error(), "gdal_translate not available") {
		t.Errorf("unexpected error: %v", err)
	}
}
