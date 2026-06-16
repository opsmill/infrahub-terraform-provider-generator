package schema

import "testing"

func TestRegistryAttributeLookup(t *testing.T) {
	r := &Registry{nodes: map[string]map[string]Attribute{
		"DctVCenter": {
			"total_vcpu":   {Kind: "Number", Optional: true},
			"vcenter_name": {Kind: "Text", Optional: false},
		},
	}}

	got, ok := r.Attribute("DctVCenter", "total_vcpu")
	if !ok {
		t.Fatal("expected total_vcpu to be found")
	}
	if got.Kind != "Number" || !got.Optional {
		t.Errorf("got %+v, want {Number true}", got)
	}

	if _, ok := r.Attribute("DctVCenter", "missing"); ok {
		t.Error("expected missing attribute to report not-found")
	}
	if _, ok := r.Attribute("NoSuchNode", "x"); ok {
		t.Error("expected missing node to report not-found")
	}
}

func TestNilRegistryReportsNotFound(t *testing.T) {
	var r *Registry // nil
	if _, ok := r.Attribute("DctVCenter", "total_vcpu"); ok {
		t.Error("nil registry must report not-found, not panic")
	}
}
