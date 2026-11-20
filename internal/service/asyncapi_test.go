package service

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// asyncMessages are the messages an AsyncAPI document names.
func asyncMessages(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Messages map[string]any `yaml:"messages"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var names []string
	for name := range doc.Components.Messages {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// TestAsyncAPIMatchesTheCode: api/asyncapi/live.yaml has a message for each
// kind of live update the code publishes, and webhooks.yaml one for each
// webhook event, and neither has any other.
func TestAsyncAPIMatchesTheCode(t *testing.T) {
	published := regexp.MustCompile(`\.publish(?:To)?\([^\n]*?"([a-z_]+)"`)
	var live []string
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range published.FindAllStringSubmatch(string(src), -1) {
			if !slices.Contains(live, m[1]) {
				live = append(live, m[1])
			}
		}
	}
	slices.Sort(live)
	if got := asyncMessages(t, "../../api/asyncapi/live.yaml"); !slices.Equal(got, live) {
		t.Errorf("api/asyncapi/live.yaml has %v; the code publishes %v", got, live)
	}

	webhooks := append(slices.Clone(WebhookEvents), WebhookTest)
	slices.Sort(webhooks)
	if got := asyncMessages(t, "../../api/asyncapi/webhooks.yaml"); !slices.Equal(got, webhooks) {
		t.Errorf("api/asyncapi/webhooks.yaml has %v; the code sends %v", got, webhooks)
	}
}
