// GDAL/COG processing: real raster conversion via the gdal_translate
// subprocess (specs/04.8 GeoTIFF/COG, docs/adr/adr-011). The processor is
// honest about its environment — when gdal_translate is not in PATH, Start
// fails with an actionable message instead of pretending; deployments that
// want real COG output install GDAL in the API image (apt-get install gdal-bin).
package remotesensing

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FileSink stores a produced raster as a tenant file and returns its id.
// Implemented at app assembly over platform.ObjectStorage + the files table.
type FileSink interface {
	// SaveOutput stores produced bytes as a file in the JOB's tenant; the
	// sink closure is tenant-scoped at app assembly.
	SaveOutput(ctx context.Context, filename, contentType string, r io.Reader) (string, error)
}

// GDALProcessor converts source rasters to COG via gdal_translate.
// job_type mapping (documented, no fabrication):
//   - orthophoto: gdal_translate -of COG (RGB/RGBA input)
//   - dem:        gdal_translate -of COG -ot Float32
//   - ndvi:       requires multispectral band math — honest TODO error.
type GDALProcessor struct {
	sink    FileSink
	storage storageFetcher
	bin     string // gdal_translate binary path (resolved at Start)
	workDir string
	log     *slog.Logger

	mu    sync.Mutex
	state map[string]*gdalState
}

type gdalState struct {
	status   string // running | succeeded | failed
	progress int
	outputs  []JobOutput
	err      string
}

// storageFetcher fetches a stored input file's bytes.
type storageFetcher interface {
	FetchInput(ctx context.Context, fileID string) (rc io.ReadCloser, filename string, err error)
}

// NewGDALProcessor resolves the gdal_translate binary; missingBinaryErr reports
// whether the binary is unavailable so callers can surface an honest error.
func NewGDALProcessor(sink FileSink, fetch storageFetcher, workDir string, log *slog.Logger) (*GDALProcessor, error) {
	bin, err := exec.LookPath("gdal_translate")
	if err != nil {
		return nil, fmt.Errorf("gdal_translate not found in PATH; install GDAL (e.g. apt-get install gdal-bin) to enable real COG processing")
	}
	if workDir == "" {
		workDir = os.TempDir()
	}
	return &GDALProcessor{
		sink: sink, storage: fetch, bin: bin, workDir: workDir,
		state: map[string]*gdalState{}, log: log,
	}, nil
}

func (p *GDALProcessor) Name() string { return "gdal" }

// buildGdalTranslateArgs constructs the COG conversion command (unit-tested).
// jobType drives output tagging; ndvi is rejected with an honest TODO.
func buildGdalTranslateArgs(jobType, inPath, outPath string) ([]string, error) {
	switch jobType {
	case "orthophoto":
		return []string{"-of", "COG", "-co", "COMPRESS=DEFLATE", "-co", "OVERVIEWS=AUTO", inPath, outPath}, nil
	case "dem":
		return []string{"-of", "COG", "-co", "COMPRESS=DEFLATE", "-ot", "Float32", inPath, outPath}, nil
	case "ndvi":
		return nil, fmt.Errorf("ndvi requires multispectral band math (gdal_calc with NIR/RED bands); not implemented — TODO")
	default:
		return nil, fmt.Errorf("unsupported job_type %q for gdal processor", jobType)
	}
}

// Start downloads inputs from storage and launches gdal_translate in a
// goroutine; Poll reports its progress.
func (p *GDALProcessor) Start(ctx context.Context, jobType string, inputs []JobInput) (string, error) {
	if len(inputs) == 0 {
		return "", fmt.Errorf("no input files")
	}
	if _, err := exec.LookPath(p.bin); err != nil {
		return "", fmt.Errorf("gdal_translate not available in PATH — real COG processing requires GDAL installed in the API image")
	}
	if _, err := buildGdalTranslateArgs(jobType, "", ""); err != nil {
		return "", err
	}

	workDir, err := os.MkdirTemp(p.workDir, "ctc-gdal-")
	if err != nil {
		return "", err
	}

	extID := "gdal-" + fmt.Sprint(time.Now().UnixNano()%1000000)
	p.mu.Lock()
	p.state[extID] = &gdalState{status: "running", progress: 10}
	p.mu.Unlock()

	go p.run(jobType, extID, workDir, inputs)
	return extID, nil
}

// run performs the actual conversion for one job (download → translate → sink).
func (p *GDALProcessor) run(jobType, extID, workDir string, inputs []JobInput) {
	defer os.RemoveAll(workDir)
	setState := func(status string, progress int, errStr string, outputs []JobOutput) {
		p.mu.Lock()
		p.state[extID] = &gdalState{status: status, progress: progress, err: errStr, outputs: outputs}
		p.mu.Unlock()
	}

	var outputs []JobOutput
	for i, in := range inputs {
		// Download input (the fetcher closure resolves the job's tenant).
		rc, filename, err := p.storage.FetchInput(context.Background(), in.FileID)
		if err != nil {
			setState("failed", 0, "input fetch failed: "+err.Error(), nil)
			return
		}
		inPath := filepath.Join(workDir, fmt.Sprintf("in-%d-%s", i, filename))
		f, err := os.Create(inPath)
		if err != nil {
			rc.Close()
			setState("failed", 0, err.Error(), nil)
			return
		}
		if _, err := io.Copy(f, rc); err != nil {
			f.Close()
			rc.Close()
			setState("failed", 0, err.Error(), nil)
			return
		}
		f.Close()
		rc.Close()

		// Convert to COG.
		outPath := filepath.Join(workDir, fmt.Sprintf("out-%d-%s.cog.tif", i, strings.TrimSuffix(filename, filepath.Ext(filename))))
		args, err := buildGdalTranslateArgs(jobType, inPath, outPath)
		if err != nil {
			setState("failed", 0, err.Error(), nil)
			return
		}
		cmd := exec.Command(p.bin, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			setState("failed", 0, fmt.Sprintf("gdal_translate: %v: %s", err, strings.TrimSpace(string(out))), nil)
			return
		}

		// Store the COG as a tenant file.
		cogFile, err := os.Open(outPath)
		if err != nil {
			setState("failed", 0, err.Error(), nil)
			return
		}
		fileID, err := p.sink.SaveOutput(context.Background(), outPath2Name(filename, jobType, i),
			"image/tiff; application=geotiff; profile=cloud-optimized", cogFile)
		cogFile.Close()
		if err != nil {
			setState("failed", 0, "output store failed: "+err.Error(), nil)
			return
		}
		outputs = append(outputs, JobOutput{FileID: fileID, Type: jobType, Name: outPath2Name(filename, jobType, i)})
		progress := 10 + (90 * (i + 1) / len(inputs))
		setState("running", progress, "", nil)
	}
	setState("succeeded", 100, "", outputs)
	p.log.Info("gdal COG conversion finished", "job", extID, "outputs", len(outputs))
}

// outPath2Name derives a friendly output filename.
func outPath2Name(srcName, jobType string, idx int) string {
	base := strings.TrimSuffix(srcName, filepath.Ext(srcName))
	return fmt.Sprintf("%s-%s-%d.cog.tif", base, jobType, idx+1)
}

// Poll reports the tracked goroutine state.
func (p *GDALProcessor) Poll(_ context.Context, _, externalID string, _ []JobInput) (JobState, error) {
	p.mu.Lock()
	st, ok := p.state[externalID]
	p.mu.Unlock()
	if !ok {
		return JobState{}, fmt.Errorf("unknown gdal job %s", externalID)
	}
	return JobState{
		Status:   st.status,
		Progress: st.progress,
		Outputs:  st.outputs,
		Err:      st.err,
	}, nil
}
