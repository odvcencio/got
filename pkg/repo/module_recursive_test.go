package repo

import "testing"

func TestModuleRecursive_CycleDetection(t *testing.T) {
	visited := map[string]bool{"github:myorg/a": true}
	err := checkModuleCycle("github:myorg/a", visited)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	t.Logf("got expected error: %v", err)
}

func TestModuleRecursive_NoCycle(t *testing.T) {
	visited := map[string]bool{"github:myorg/a": true}
	err := checkModuleCycle("github:myorg/b", visited)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestModuleRecursive_DepthLimit(t *testing.T) {
	err := checkDepthLimit(11, 10)
	if err == nil {
		t.Fatal("expected depth limit error, got nil")
	}
	t.Logf("got expected error: %v", err)
}

func TestModuleRecursive_DepthOK(t *testing.T) {
	err := checkDepthLimit(5, 10)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}
