package gui

import (
	"testing"
	"time"

	appcore "zenhub/internal/client/app"
)

func TestTraySyncStatusText(t *testing.T) {
	if got := traySyncStatusText(appcore.StatusView{}); got != "disabled" {
		t.Fatalf("traySyncStatusText(disabled) = %q, want disabled", got)
	}

	status := appcore.StatusView{SyncEnabled: true, LastSyncStatus: "updated"}
	if got := traySyncStatusText(status); got != "updated" {
		t.Fatalf("traySyncStatusText(updated) = %q, want updated", got)
	}

	status = appcore.StatusView{SyncEnabled: true}
	if got := traySyncStatusText(status); got != "enabled" {
		t.Fatalf("traySyncStatusText(enabled without status) = %q, want enabled", got)
	}
}

func TestTrayConflictText(t *testing.T) {
	if got := trayConflictText(appcore.SyncConflictView{}); got != "none" {
		t.Fatalf("trayConflictText(no conflict) = %q, want none", got)
	}

	conflict := appcore.SyncConflictView{HasConflict: true, Reason: "pull_conflict"}
	if got := trayConflictText(conflict); got != "Both local and cloud snapshots changed since the last sync." {
		t.Fatalf("trayConflictText(pull_conflict) = %q", got)
	}
}

func TestTrayErrorTextPriority(t *testing.T) {
	status := appcore.StatusView{ServiceError: "service down", SyncError: "sync failed"}
	syncState := appcore.SyncStateView{Error: "summary error"}
	if got := trayErrorText(status, syncState); got != "service down" {
		t.Fatalf("trayErrorText(service first) = %q, want service down", got)
	}

	status = appcore.StatusView{SyncError: "sync failed"}
	if got := trayErrorText(status, syncState); got != "sync failed" {
		t.Fatalf("trayErrorText(sync second) = %q, want sync failed", got)
	}

	status = appcore.StatusView{}
	if got := trayErrorText(status, syncState); got != "summary error" {
		t.Fatalf("trayErrorText(summary third) = %q, want summary error", got)
	}

	if got := trayErrorText(appcore.StatusView{}, appcore.SyncStateView{}); got != "none" {
		t.Fatalf("trayErrorText(empty) = %q, want none", got)
	}
}

func TestTrayTimeText(t *testing.T) {
	if got := trayTimeText(time.Time{}, "never"); got != "never" {
		t.Fatalf("trayTimeText(zero) = %q, want never", got)
	}

	value := time.Date(2026, time.May, 12, 9, 8, 7, 0, time.UTC)
	if got := trayTimeText(value, "never"); got != "2026-05-12 09:08:07" {
		t.Fatalf("trayTimeText(value) = %q", got)
	}
}

func TestCodexProviderText(t *testing.T) {
	if got := codexProviderText(nil); got != "none" {
		t.Fatalf("codexProviderText(nil) = %q, want none", got)
	}

	providers := []appcore.CodexProviderView{
		{Name: "provider-a", Current: false},
		{Name: "provider-b", Current: false},
	}
	if got := codexProviderText(providers); got != "not selected" {
		t.Fatalf("codexProviderText(no current) = %q, want not selected", got)
	}

	providers[1].Current = true
	if got := codexProviderText(providers); got != "provider-b" {
		t.Fatalf("codexProviderText(current) = %q, want provider-b", got)
	}
}
