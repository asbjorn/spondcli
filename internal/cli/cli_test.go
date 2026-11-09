package cli

import (
	"context"
	"errors"
	"testing"
)

func TestRunUsageErrors(t *testing.T) {
	ctx := context.Background()
	for _, args := range [][]string{
		{"nosuch"},
		{"members", "-nosuchflag"},
		{"members", "-group", "x", "extra"},
		{"search", "-group", "x"},
		{"logout", "extra"},
		{"help", "nosuch"},
	} {
		if err := Run(ctx, args); !errors.Is(err, ErrUsage) {
			t.Errorf("Run(%q) = %v, want a usage error", args, err)
		}
	}
}

func TestRunHelp(t *testing.T) {
	ctx := context.Background()
	for _, args := range [][]string{
		{"-h"},
		{"--help"},
		{"help"},
		{"help", "members"},
		{"help", "search"},
		{"help", "version"},
		{"members", "-h"},
		{"version"},
		{"--version"},
	} {
		if err := Run(ctx, args); err != nil {
			t.Errorf("Run(%q) = %v, want nil", args, err)
		}
	}
}

func TestBuildVersion(t *testing.T) {
	old := version
	defer func() { version = old }()
	version = "v1.2.3"
	if got := buildVersion(); got != "v1.2.3" {
		t.Errorf("buildVersion() = %q, want v1.2.3", got)
	}
}
