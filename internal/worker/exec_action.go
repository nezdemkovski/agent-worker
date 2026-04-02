package worker

import (
	"context"
	"fmt"
)

func RunExecAction(ctx context.Context, payload ExecActionPayload) (*ExecActionResult, error) {
	if len(payload.Command) == 0 {
		return nil, fmt.Errorf("missing command")
	}
	result, err := RunCommand(ctx, "", nil, "", payload.Command[0], payload.Command[1:]...)
	if err != nil {
		return &ExecActionResult{
			Command:  append([]string(nil), payload.Command...),
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
			ExitCode: result.ExitCode,
		}, err
	}
	return &ExecActionResult{
		Command:  append([]string(nil), payload.Command...),
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: 0,
	}, nil
}
