package worker

// TypedStartPlan is a structured, JSON-serializable service startup plan.
// It replaces opaque shell command strings with explicit checks, steps,
// environment, and fallback behavior.
type TypedStartPlan struct {
	RuntimeProfile string            `json:"runtime_profile"`
	Strategy       string            `json:"strategy"`
	Workdir        string            `json:"workdir"`
	Env            map[string]string `json:"env,omitempty"`
	Checks         []PlanCheck       `json:"checks"`
	Steps          []PlanStep        `json:"steps"`
	Fallback       []PlanStep        `json:"fallback,omitempty"`
	Description    string            `json:"description"`
}

// PlanCheck is a precondition that must pass before steps execute.
type PlanCheck struct {
	Type string `json:"type"` // "dir_exists", "file_exists", "command_exists"
	Path string `json:"path,omitempty"`
	Name string `json:"name,omitempty"`
}

// PlanStep is a single command to run as part of the startup plan.
type PlanStep struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Workdir string            `json:"workdir,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Exec    bool              `json:"exec,omitempty"` // replace the current process (last step)
}
