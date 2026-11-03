package main

import (
	"flag"
	"strings"
	"testing"
)

func parsed(args ...string) *flag.FlagSet {
	fs := flag.NewFlagSet("translator-eval", flag.ContinueOnError)
	fs.Int("repeats", 1, "")
	fs.String("models", "", "")
	fs.Parse(args)
	return fs
}

func TestStray_RefusesASubcommandAfterAFlag(t *testing.T) {
	args := []string{"-repeats", "5", "matrix", "-models", "anthropic-small:low"}
	err := stray(parsed(args...), args)
	if err == nil {
		t.Fatal("a matrix after -repeats ran as one eval")
	}
	if want := "translator-eval matrix -repeats 5 -models anthropic-small:low"; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to show %q", err, want)
	}
}

func TestStray_RefusesAnyOtherWordLeftOver(t *testing.T) {
	args := []string{"-repeats", "5", "anthropic-small"}
	if err := stray(parsed(args...), args); err == nil {
		t.Error("a word left over was dropped without a word")
	}
}

func TestStray_AllowsFlagsAlone(t *testing.T) {
	args := []string{"-repeats", "5", "-models", "anthropic-small:low"}
	if err := stray(parsed(args...), args); err != nil {
		t.Errorf("flags alone refused: %v", err)
	}
}
