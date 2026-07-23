package main

import (
	"strings"
	"testing"
)

func TestLoadConfigRequiresAllowedProjects(t *testing.T) {
	t.Setenv("JCM_GEOIP_HMAC_SECRET", "test-secret")
	t.Setenv("JCM_GEOIP_ALLOWED_PROJECTS", "")

	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "JCM_GEOIP_ALLOWED_PROJECTS") {
		t.Fatalf("expected an allowed-projects configuration error, got %v", err)
	}
}

func TestLoadConfigParsesStableAllowedProjects(t *testing.T) {
	t.Setenv("JCM_GEOIP_HMAC_SECRET", "test-secret")
	t.Setenv("JCM_GEOIP_ALLOWED_PROJECTS", "vebou, second-store,vebou")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig returned an error: %v", err)
	}
	if len(cfg.allowedProjects) != 2 {
		t.Fatalf("expected two unique projects, got %d", len(cfg.allowedProjects))
	}
	if _, ok := cfg.allowedProjects["vebou"]; !ok {
		t.Fatal("vebou project was not retained")
	}
	if _, ok := cfg.allowedProjects["second-store"]; !ok {
		t.Fatal("second-store project was not retained")
	}
}
