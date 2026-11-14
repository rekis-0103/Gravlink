package agy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// DoctorCheck represents a single Antigravity compatibility verification item.
type DoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // PASS, WARN, FAIL
	Detail string `json:"detail"`
}

// RunDoctor executes contract and environment checks against the local Antigravity installation.
func RunDoctor(ctx context.Context, antigravityDir, workspace string) []DoctorCheck {
	return RunDoctorWithPatches(ctx, antigravityDir, workspace, "")
}

// RunDoctorWithPatches runs all Antigravity compatibility checks plus external hot-reload patch directory status.
func RunDoctorWithPatches(ctx context.Context, antigravityDir, workspace, patchDir string) []DoctorCheck {
	checks := make([]DoctorCheck, 0, 6)

	// 1. Antigravity data directory
	if info, err := os.Stat(antigravityDir); err != nil || !info.IsDir() {
		checks = append(checks, DoctorCheck{
			Name:   "antigravity_data_dir",
			Status: "FAIL",
			Detail: fmt.Sprintf("directory not accessible: %s", antigravityDir),
		})
	} else {
		checks = append(checks, DoctorCheck{
			Name:   "antigravity_data_dir",
			Status: "PASS",
			Detail: antigravityDir,
		})
	}

	// 2. agy binary
	agyBin := FindAgyBinary()
	if agyBin == "" {
		checks = append(checks, DoctorCheck{
			Name:   "agy_binary",
			Status: "WARN",
			Detail: "agy binary not found in ~/.gemini/bin or PATH (CLI fallback unavailable)",
		})
	} else {
		checks = append(checks, DoctorCheck{
			Name:   "agy_binary",
			Status: "PASS",
			Detail: agyBin,
		})
	}

	// 3. Running Hub discovery (daemon/hub_*.json + /healthz)
	hubs, err := DiscoverHubs(ctx, antigravityDir)
	if err != nil {
		checks = append(checks, DoctorCheck{
			Name:   "hub_discovery",
			Status: "WARN",
			Detail: fmt.Sprintf("discovery error: %v", err),
		})
	} else if len(hubs) == 0 {
		checks = append(checks, DoctorCheck{
			Name:   "hub_discovery",
			Status: "WARN",
			Detail: "no live agy --hub daemon responded on /healthz (open Antigravity IDE to enable live hub)",
		})
	} else {
		h := hubs[0]
		checks = append(checks, DoctorCheck{
			Name:   "hub_discovery",
			Status: "PASS",
			Detail: fmt.Sprintf("detected %d live hub(s); active port=%d lsVersion=%s pid=%d", len(hubs), h.HTTPPort, h.LSVersion, h.PID),
		})
	}

	// 4. Transcript contract check
	convs, err := ListConversations(antigravityDir, 5)
	if err != nil {
		checks = append(checks, DoctorCheck{
			Name:   "transcript_contract",
			Status: "FAIL",
			Detail: fmt.Sprintf("failed listing conversations: %v", err),
		})
	} else if len(convs) == 0 {
		checks = append(checks, DoctorCheck{
			Name:   "transcript_contract",
			Status: "WARN",
			Detail: "no conversations found in brain/ yet",
		})
	} else {
		latest := convs[0]
		tPath := TranscriptPath(antigravityDir, latest.ID, false)
		if err := ValidateTranscriptContract(tPath); err != nil {
			checks = append(checks, DoctorCheck{
				Name:   "transcript_contract",
				Status: "FAIL",
				Detail: fmt.Sprintf("schema mismatch in %s: %v", filepath.Base(latest.ID), err),
			})
		} else {
			checks = append(checks, DoctorCheck{
				Name:   "transcript_contract",
				Status: "PASS",
				Detail: fmt.Sprintf("found %d recent session(s); latest=%s (%d steps)", len(convs), latest.ID[:min(8, len(latest.ID))], latest.StepCount),
			})
		}
	}

	// 5. Workspace root
	if workspace != "" {
		if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
			checks = append(checks, DoctorCheck{
				Name:   "workspace_root",
				Status: "FAIL",
				Detail: fmt.Sprintf("workspace not accessible: %s", workspace),
			})
		} else {
			checks = append(checks, DoctorCheck{
				Name:   "workspace_root",
				Status: "PASS",
				Detail: workspace,
			})
		}
	}

	// 6. External hot-reloadable patches (Milestone 4)
	checks = append(checks, CheckExternalPatches(patchDir))

	return checks
}

// CheckExternalPatches verifies whether external mobile-hub CSS/JS overrides exist on disk.
func CheckExternalPatches(patchDir string) DoctorCheck {
	if patchDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			patchDir = filepath.Join(home, ".gravlink", "patches")
		}
	}
	if patchDir == "" {
		return DoctorCheck{
			Name:   "external_patches",
			Status: "PASS",
			Detail: "using embedded mobile-hub patches",
		}
	}
	var overrides []string
	for _, name := range []string{"mobile-hub.css", "mobile-hub.js"} {
		if info, err := os.Stat(filepath.Join(patchDir, name)); err == nil && !info.IsDir() {
			overrides = append(overrides, name)
		}
	}
	if len(overrides) > 0 {
		return DoctorCheck{
			Name:   "external_patches",
			Status: "PASS",
			Detail: fmt.Sprintf("hot-reload overrides active in %s (%v)", patchDir, overrides),
		}
	}
	return DoctorCheck{
		Name:   "external_patches",
		Status: "PASS",
		Detail: fmt.Sprintf("embedded fallback active (drop overrides into %s)", patchDir),
	}
}
