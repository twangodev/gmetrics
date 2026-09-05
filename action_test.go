package gmetrics_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func actionScript(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("action.yml")
	if err != nil {
		t.Fatal(err)
	}
	var action struct {
		Runs struct {
			Steps []struct{ Name, Run string }
		}
	}
	if err := yaml.Unmarshal(data, &action); err != nil {
		t.Fatal(err)
	}
	for _, step := range action.Runs.Steps {
		if step.Name == "Run gmetrics" {
			return step.Run
		}
	}
	t.Fatal("Run gmetrics step missing")
	return ""
}

func TestActionRefs(t *testing.T) {
	script := actionScript(t)
	for _, ref := range []string{"main", "v1", "v12", "v1.8.0", strings.Repeat("a", 40), "", "feature/example", "feature", "v1.8", "v1.8.0-rc.1", "abc1234", "$(exit 42)"} {
		t.Run(ref, func(t *testing.T) {
			dir := t.TempDir()
			capture := filepath.Join(dir, "arguments")
			stub := "#!/bin/bash\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\nprintf '%s' \"$GMETRICS_INPUTS\" > \"$CAPTURE.inputs\"\nexit \"${DOCKER_EXIT:-0}\"\n"
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(stub), 0o755); err != nil {
				t.Fatal(err)
			}
			inputs := `{"token":"test token","user":"test-user"}`
			workspace := filepath.Join(dir, "workspace with spaces")
			valid := ref == "main" || ref == "v1" || ref == "v12" || ref == "v1.8.0" || ref == strings.Repeat("a", 40)
			for _, dockerExit := range []string{"0", "17"} {
				cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", script)
				cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "CAPTURE="+capture,
					"GMETRICS_IMAGE_TAG="+ref, "GMETRICS_INPUTS="+inputs, "GITHUB_WORKSPACE="+workspace, "DOCKER_EXIT="+dockerExit)
				output, err := cmd.CombinedOutput()
				if !valid {
					if err == nil || !strings.Contains(string(output), "::error::") {
						t.Fatalf("unsupported ref should fail with diagnostic: %s, %v", output, err)
					}
					if _, err := os.Stat(capture); !os.IsNotExist(err) {
						t.Fatal("unsupported ref invoked Docker")
					}
					continue
				}
				if dockerExit == "0" && err != nil {
					t.Fatalf("supported ref failed: %s, %v", output, err)
				}
				if dockerExit == "17" && (cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 17) {
					t.Fatalf("Docker failure was not propagated: %s, %v", output, err)
				}
				data, err := os.ReadFile(capture)
				if err != nil {
					t.Fatal(err)
				}
				want := []string{"run", "--rm", "--pull=always", "--env", "CI=true", "--env", "GMETRICS_INPUTS",
					"--volume", workspace + ":/github/workspace", "--workdir", "/github/workspace", "ghcr.io/twangodev/gmetrics:" + ref}
				if got := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"); !reflect.DeepEqual(got, want) {
					t.Fatalf("Docker arguments: got %q, want %q", got, want)
				}
				data, err = os.ReadFile(capture + ".inputs")
				if err != nil || string(data) != inputs {
					t.Fatalf("inputs were changed: %q, %v", data, err)
				}
			}
		})
	}
}

// Exercise the real image's entrypoint and input parsing without credentials or
// network requests. Ref selection and pull behavior are covered above; only the
// image name and pull policy are replaced to use the locally built candidate.
func TestActionContainer(t *testing.T) {
	image := os.Getenv("GMETRICS_TEST_IMAGE")
	if image == "" {
		t.Skip("set GMETRICS_TEST_IMAGE to a locally built image")
	}
	script := strings.ReplaceAll(actionScript(t), "--pull=always", "--pull=never")
	script = strings.ReplaceAll(script, "ghcr.io/twangodev/gmetrics:${GMETRICS_IMAGE_TAG}", "${GMETRICS_TEST_IMAGE}")
	cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", script)
	cmd.Env = append(os.Environ(), "GMETRICS_IMAGE_TAG=main", "GITHUB_WORKSPACE="+t.TempDir(),
		`GMETRICS_INPUTS={"user":"action-smoke-test"}`)
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "github token required") {
		t.Fatalf("expected entrypoint to parse user input and reject missing token: %s, %v", output, err)
	}
}
