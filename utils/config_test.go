package utils

import (
	"os"
	"testing"
)

func TestHasConfigArg(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{name: "short separated", args: []string{"-c", "custom.yaml"}, want: true},
		{name: "short equals", args: []string{"-c=custom.yaml"}, want: true},
		{name: "long separated", args: []string{"--c", "custom.yaml"}, want: true},
		{name: "long equals", args: []string{"--c=custom.yaml"}, want: true},
		{name: "missing value", args: []string{"-c"}, want: false},
		{name: "none", args: []string{"serve"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasConfigArg(tc.args); got != tc.want {
				t.Fatalf("HasConfigArg(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestSetDefaultConfigEnvRespectsExplicitEnv(t *testing.T) {
	t.Setenv("NAV_CONFIG", "custom.yaml")
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"freeai"}

	NewDefaultConfigManager([]byte("system:\n  app-name: test\n")).SetDefaultConfigEnv("embedded.yaml")

	if got := os.Getenv("NAV_CONFIG"); got != "custom.yaml" {
		t.Fatalf("NAV_CONFIG = %q, want explicit env", got)
	}
}

func TestSetDefaultConfigEnvRespectsConfigArg(t *testing.T) {
	t.Setenv("NAV_CONFIG", "")
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"freeai", "-c", "custom.yaml"}

	NewDefaultConfigManager([]byte("system:\n  app-name: test\n")).SetDefaultConfigEnv("embedded.yaml")

	if got := os.Getenv("NAV_CONFIG"); got != "" {
		t.Fatalf("NAV_CONFIG = %q, want empty because -c is explicit", got)
	}
}

func TestSetDefaultConfigEnvRespectsLocalConfig(t *testing.T) {
	t.Setenv("NAV_CONFIG", "")
	oldArgs := os.Args
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		os.Args = oldArgs
		_ = os.Chdir(oldWd)
	})
	os.Args = []string{"freeai"}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.WriteFile("config.yaml", []byte("system:\n  app-name: test\n"), 0600); err != nil {
		t.Fatalf("write local config: %v", err)
	}

	NewDefaultConfigManager([]byte("system:\n  app-name: test\n")).SetDefaultConfigEnv("embedded.yaml")

	if got := os.Getenv("NAV_CONFIG"); got != "" {
		t.Fatalf("NAV_CONFIG = %q, want empty because local config exists", got)
	}
}
