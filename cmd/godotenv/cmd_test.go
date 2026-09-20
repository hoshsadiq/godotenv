package main

import (
	"errors"
	"flag"
	"reflect"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		want     cliOptions
		wantHelp bool
		wantErr  bool
	}{
		{
			name: "no args defaults to .env and no command",
			args: nil,
			want: cliOptions{filenames: []string{".env"}},
		},
		{
			name: "a single -f replaces the default",
			args: []string{"-f", "other.env", "echo", "hi"},
			want: cliOptions{filenames: []string{"other.env"}, command: "echo", cmdArgs: []string{"hi"}},
		},
		{
			name: "repeated -f keeps order",
			args: []string{"-f", "one.env", "-f", "two.env", "cmd"},
			want: cliOptions{filenames: []string{"one.env", "two.env"}, command: "cmd"},
		},
		{
			name: "override flag",
			args: []string{"-o", "-f", "x.env", "cmd"},
			want: cliOptions{filenames: []string{"x.env"}, override: true, command: "cmd"},
		},
		{
			name: "version flag",
			args: []string{"-v"},
			want: cliOptions{filenames: []string{".env"}, version: true},
		},
		{
			name:    "unknown flag is an error",
			args:    []string{"--nope", "cmd"},
			wantErr: true,
		},
		{
			name:    "malformed flag is an error",
			args:    []string{"-f"},
			wantErr: true,
		},
		{
			name:     "help is signalled with ErrHelp",
			args:     []string{"-h"},
			wantHelp: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseArgs(tt.args)
			if tt.wantHelp {
				if !errors.Is(err, flag.ErrHelp) {
					t.Fatalf("expected flag.ErrHelp, got %v", err)
				}
				return
			}
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if errors.Is(err, flag.ErrHelp) {
					t.Fatal("expected a parse error, got ErrHelp")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tt.want, *got) {
				t.Errorf("expected %+v, got %+v", tt.want, *got)
			}
		})
	}
}

func TestUsageDescribesRepeatableFileFlag(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	printUsage(&buf)
	out := buf.String()

	if !strings.Contains(out, "Repeat for multiple files") {
		t.Errorf("usage should say -f can be repeated, got:\n%s", out)
	}
	if strings.Contains(out, "Comma separated") {
		t.Errorf("usage should not mention comma separated paths, got:\n%s", out)
	}
}
