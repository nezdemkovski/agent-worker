package worker

type CheckType string

const (
	CheckDirExists     CheckType = "dir_exists"
	CheckFileExists    CheckType = "file_exists"
	CheckCommandExists CheckType = "command_exists"
)

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

type PlanCheck struct {
	Type CheckType `json:"type"`
	Path string    `json:"path,omitempty"`
	Name string    `json:"name,omitempty"`
}

type PlanStep struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Workdir string            `json:"workdir,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Exec    bool              `json:"exec,omitempty"`
}
