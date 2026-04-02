package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

type outputMode struct {
	json bool
}

func emit(fields map[string]string, mode outputMode) {
	if mode.json {
		data, err := json.Marshal(fields)
		if err != nil {
			fmt.Fprintf(os.Stdout, "{\"status\":\"error\",\"reason\":%q}\n", "failed to encode JSON output")
			return
		}
		fmt.Fprintln(os.Stdout, string(data))
		return
	}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Printf("%s=%s\n", key, fields[key])
	}
}
