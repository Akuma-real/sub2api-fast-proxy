package proxy

import (
	"encoding/json"
	"testing"
)

func TestForceTopLevelStringPropertyAddsServiceTier(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":[{"role":"user","content":"hi"}]}`)
	got, changed, err := forceTopLevelStringProperty(body, "service_tier", "priority")
	if err != nil {
		t.Fatalf("forceTopLevelStringProperty returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}

	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %s: %v", string(got), err)
	}
	if parsed["service_tier"] != "priority" {
		t.Fatalf("service_tier = %v, want priority", parsed["service_tier"])
	}
	if parsed["model"] != "gpt-5.5" {
		t.Fatalf("model changed: %v", parsed["model"])
	}
}

func TestForceTopLevelStringPropertyReplacesOnlyTopLevelKey(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","nested":{"service_tier":"flex"},"service_tier":"auto"}`)
	got, _, err := forceTopLevelStringProperty(body, "service_tier", "priority")
	if err != nil {
		t.Fatalf("forceTopLevelStringProperty returned error: %v", err)
	}

	var parsed struct {
		ServiceTier string `json:"service_tier"`
		Nested      struct {
			ServiceTier string `json:"service_tier"`
		} `json:"nested"`
	}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %s: %v", string(got), err)
	}
	if parsed.ServiceTier != "priority" {
		t.Fatalf("top-level service_tier = %q, want priority", parsed.ServiceTier)
	}
	if parsed.Nested.ServiceTier != "flex" {
		t.Fatalf("nested service_tier = %q, want flex", parsed.Nested.ServiceTier)
	}
}

func TestForceTopLevelStringPropertyHandlesEmptyObject(t *testing.T) {
	got, _, err := forceTopLevelStringProperty([]byte("{\n  }"), "service_tier", "priority")
	if err != nil {
		t.Fatalf("forceTopLevelStringProperty returned error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %s: %v", string(got), err)
	}
	if parsed["service_tier"] != "priority" {
		t.Fatalf("service_tier = %v, want priority", parsed["service_tier"])
	}
}

func TestForceTopLevelStringPropertyRejectsNonObject(t *testing.T) {
	_, _, err := forceTopLevelStringProperty([]byte(`[]`), "service_tier", "priority")
	if err == nil {
		t.Fatalf("expected error")
	}
}
