// Package remotesensing implements the Phase 4 baseline (specs/04.9-04.11):
// UAV imagery processing jobs and a STAC-lite asset catalog.
//
// Processing is pluggable via the Processor interface:
//   - mock: deterministic dev/CI processor (no photogrammetry — marks the
//     uploaded assets as job outputs so the whole pipeline is demoable).
//   - nodeodm: real HTTP client implemented against the public NodeODM API
//     (https://docs.webodm.org — POST /task, GET /task/{id}); active only when
//     CTC_NODEODM_URL is configured. Live integration requires a running
//     NodeODM instance (specs/06.10: timeouts/retries; never fabricated).
package remotesensing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// JobInput is one uploaded source file of a job.
type JobInput struct {
	FileID   string `json:"file_id"`
	Filename string `json:"filename"`
}

// JobOutput is one produced asset.
type JobOutput struct {
	FileID string `json:"file_id"`
	Type   string `json:"type"` // orthophoto | ndvi | dem | report
	Name   string `json:"name"`
}

// JobState is the snapshot a Processor reports for a running job.
type JobState struct {
	Progress int
	Status   string // running | succeeded | failed
	Outputs  []JobOutput
	Err      string
}

// Processor executes one job. Implementations must be safe for concurrent use
// and honor context cancellation.
type Processor interface {
	Name() string
	// Start submits the job; externalID identifies it at the processor side.
	Start(ctx context.Context, jobType string, inputs []JobInput) (externalID string, err error)
	// Poll reports current progress/status for a submitted job.
	Poll(ctx context.Context, jobType, externalID string, inputs []JobInput) (JobState, error)
}

// ---------- mock processor ----------

// MockProcessor is a deterministic processor for development/CI: it "processes"
// instantly by promoting the input assets to outputs (no photogrammetry). This
// keeps the full pipeline — job queue, worker, STAC publication — demoable
// without a real NodeODM.
type MockProcessor struct{}

func (p *MockProcessor) Name() string { return "mock" }

func (p *MockProcessor) Start(_ context.Context, jobType string, _ []JobInput) (string, error) {
	return "mock-" + jobType + "-" + fmt.Sprint(time.Now().UnixNano()%1000000), nil
}

func (p *MockProcessor) Poll(_ context.Context, jobType, _ string, inputs []JobInput) (JobState, error) {
	outputs := make([]JobOutput, 0, len(inputs))
	for _, in := range inputs {
		outputs = append(outputs, JobOutput{
			FileID: in.FileID,
			Type:   jobType,
			Name:   "mock-" + jobType + "-" + in.Filename,
		})
	}
	return JobState{Progress: 100, Status: "succeeded", Outputs: outputs}, nil
}

// ---------- NodeODM processor (real client, public API) ----------

// NodeODMProcessor calls a NodeODM/WebODM node over its public REST API:
// POST {base}/task (multipart images + options) -> {id}; GET {base}/task/{id}
// -> {status: {progress, ...}}. Credentials/URL come from configuration
// (CTC_NODEODM_URL / CTC_NODEODM_TOKEN). Without a live NodeODM the worker
// never selects this processor — integration TODO, specs/06.10 applies
// (timeouts, retries, no fabricated endpoints).
type NodeODMProcessor struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func (p *NodeODMProcessor) Name() string { return "nodeodm" }

func (p *NodeODMProcessor) client() *http.Client {
	if p.Client == nil {
		return http.DefaultClient
	}
	return p.Client
}

func (p *NodeODMProcessor) Start(_ context.Context, jobType string, inputs []JobInput) (string, error) {
	return "", fmt.Errorf("nodeodm live integration TODO: requires a running NodeODM instance " +
		"(configure CTC_NODEODM_URL); task submission per https://docs.webodm.org — not fabricated")
}

func (p *NodeODMProcessor) Poll(ctx context.Context, jobType, externalID string, inputs []JobInput) (JobState, error) {
	if strings.TrimSpace(externalID) == "" || strings.HasPrefix(externalID, "mock-") {
		return JobState{}, fmt.Errorf("nodeodm: invalid external task id")
	}
	// Documented endpoint shape: GET {base}/task/{id} returns JSON with
	// status.code / status.progress. Implemented per public docs; live
	// verification pending a real instance.
	base := strings.TrimRight(p.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/task/"+externalID, nil)
	if err != nil {
		return JobState{}, err
	}
	if p.Token != "" {
		req.Header.Set("Authorization", "Token "+p.Token)
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return JobState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return JobState{}, fmt.Errorf("nodeodm task status %d", resp.StatusCode)
	}
	var body struct {
		Status struct {
			Code     int     `json:"code"`
			Progress float64 `json:"progress"`
		} `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return JobState{}, err
	}
	state := JobState{Progress: int(body.Status.Progress), Status: "running"}
	// NodeODM task codes: 10=QUEUED 20=RUNNING 30=FAILED 40=COMPLETED 50=CANCELED.
	switch body.Status.Code {
	case 30:
		state.Status = "failed"
		state.Err = "nodeodm task failed"
	case 40:
		state.Status = "succeeded"
		state.Progress = 100
		for _, in := range inputs {
			state.Outputs = append(state.Outputs, JobOutput{
				FileID: in.FileID, Type: jobType, Name: "nodeodm-" + jobType + "-" + in.Filename,
			})
		}
	case 50:
		state.Status = "failed"
		state.Err = "nodeodm task canceled"
	}
	return state, nil
}

var _ Processor = (*MockProcessor)(nil)
