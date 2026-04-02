package main

import (
	"flag"
	"fmt"
)

func flagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})
	return fs
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

func parseCommandArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	for i, arg := range args {
		if arg == "--" {
			if err := fs.Parse(args[:i]); err != nil {
				return nil, err
			}
			if len(args[i+1:]) == 0 {
				return nil, fmt.Errorf("command is required after --")
			}
			return args[i+1:], nil
		}
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() == 0 {
		return nil, fmt.Errorf("command is required after --")
	}
	return fs.Args(), nil
}
