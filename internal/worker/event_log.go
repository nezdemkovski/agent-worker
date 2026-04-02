package worker

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const eventLogFlushInterval = 2 * time.Second

type eventLogFile struct {
	path      string
	jsonlPath string
	log       *EventLog
	out       io.Writer
	file      *os.File
	lastFlush time.Time
}

func newEventLogFile(path string, out io.Writer) (*eventLogFile, error) {
	log := NewEventLog()
	if out == nil {
		out = io.Discard
	}
	file, err := os.OpenFile(path+".jsonl", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	f := &eventLogFile{
		path:      path,
		jsonlPath: path + ".jsonl",
		log:       log,
		out:       out,
		file:      file,
	}
	if err := f.flush(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return f, nil
}

func (f *eventLogFile) Append(event Event) error {
	f.log.Events = append(f.log.Events, event)
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := f.file.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := f.file.Sync(); err != nil {
		return err
	}
	if shouldFlushEventLog(event, f.lastFlush) {
		if err := f.flush(); err != nil {
			return err
		}
	}
	if f.out != nil {
		_, _ = fmt.Fprintln(f.out, formatEventLogLine(event))
	}
	return nil
}

func (f *eventLogFile) Close() error {
	if f == nil {
		return nil
	}
	if err := f.flush(); err != nil {
		_ = f.file.Close()
		return err
	}
	if f.file != nil {
		return f.file.Close()
	}
	return nil
}

func shouldFlushEventLog(event Event, lastFlush time.Time) bool {
	if event.Level == LevelError {
		return true
	}
	if lastFlush.IsZero() {
		return true
	}
	return time.Since(lastFlush) >= eventLogFlushInterval
}

func (f *eventLogFile) flush() error {
	data, err := json.Marshal(f.log)
	if err != nil {
		return err
	}
	if err := os.WriteFile(f.path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	f.lastFlush = time.Now()
	return nil
}

func formatEventLogLine(event Event) string {
	scope := event.Kind
	switch {
	case event.Service != "":
		scope = fmt.Sprintf("service %s", event.Service)
	case event.Repo != "":
		scope = fmt.Sprintf("repo %s", event.Repo)
	case scope == "":
		scope = "worker"
	}

	line := fmt.Sprintf("%s [%s] %s: %s", event.Time, event.Level, scope, event.Message)
	if len(event.Details) == 0 {
		return line
	}

	keys := make([]string, 0, len(event.Details))
	for key := range event.Details {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var detailParts []string
	for _, key := range keys {
		detailParts = append(detailParts, fmt.Sprintf("%s=%s", key, formatLogDetailValue(event.Details[key])))
	}
	return fmt.Sprintf("%s (%s)", line, strings.Join(detailParts, ", "))
}

func formatLogDetailValue(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " ,=()") {
		return strconv.Quote(value)
	}
	return value
}
