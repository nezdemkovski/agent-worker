package worker

import "encoding/json"

type ControlAction string

const (
	ActionRestart ControlAction = "restart"
	ActionExec    ControlAction = "exec"
	ActionRequest ControlAction = "request"
	ActionPrompt  ControlAction = "prompt"
)

type ControlRequest struct {
	Version   int             `json:"version"`
	RequestID string          `json:"request_id"`
	Action    ControlAction   `json:"action"`
	Service   string          `json:"service"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type ControlResponse struct {
	Version   int             `json:"version"`
	RequestID string          `json:"request_id"`
	Action    ControlAction   `json:"action"`
	Service   string          `json:"service"`
	Status    string          `json:"status"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *ControlError   `json:"error,omitempty"`
}

type ControlError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type RestartActionResult struct {
	OldPID          int    `json:"old_pid"`
	NewPID          int    `json:"new_pid"`
	URL             string `json:"url"`
	ReadyURL        string `json:"ready_url"`
	OldSourceHash   string `json:"old_source_hash"`
	NewSourceHash   string `json:"new_source_hash"`
	OldCmdline      string `json:"old_cmdline"`
	NewCmdline      string `json:"new_cmdline"`
	StatusCode      int    `json:"status_code,omitempty"`
	ResponseHeaders string `json:"response_headers,omitempty"`
	ResponseBody    string `json:"response_body,omitempty"`
}

type RequestActionPayload struct {
	URL     string   `json:"url"`
	Method  string   `json:"method"`
	Headers []string `json:"headers,omitempty"`
	Body    string   `json:"body,omitempty"`
}

type RequestActionResult struct {
	URL        string `json:"url"`
	Method     string `json:"method"`
	StatusCode int    `json:"status_code"`
	Headers    string `json:"headers"`
	Body       string `json:"body"`
}

type ExecActionPayload struct {
	Command []string `json:"command"`
}

type ExecActionResult struct {
	Command  []string `json:"command"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
	ExitCode int      `json:"exit_code"`
}

type PromptActionPayload struct {
	Tool    string `json:"tool"`
	Repo    string `json:"repo"`
	RepoDir string `json:"repo_dir"`
	Prompt  string `json:"prompt"`
}

type PromptActionResult struct {
	Tool         string   `json:"tool"`
	Repo         string   `json:"repo"`
	PromptSHA256 string   `json:"prompt_sha256"`
	Output       string   `json:"output"`
	Stderr       string   `json:"stderr"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	Command      []string `json:"command,omitempty"`
	ExitCode     int      `json:"exit_code"`
}

func NewControlResponse(req *ControlRequest, status string) *ControlResponse {
	return &ControlResponse{
		Version:   1,
		RequestID: req.RequestID,
		Action:    req.Action,
		Service:   req.Service,
		Status:    status,
	}
}

func NewControlErrorResponse(req *ControlRequest, code, message string) *ControlResponse {
	resp := NewControlResponse(req, StatusError)
	resp.Error = &ControlError{Code: code, Message: message}
	return resp
}

func (r *ControlResponse) SetResult(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	r.Result = data
	return nil
}
