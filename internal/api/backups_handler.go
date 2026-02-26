package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) GetBackupConfig(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	cfg, err := h.backupService.GetConfig(r.Context(), user.Username)
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, cfg)
}

func (h *Handler) SaveBackupConfig(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	var req SaveBackupConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg, err := h.backupService.SaveConfig(r.Context(), user.Username, &req)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, cfg)
}

func (h *Handler) TestBackupConfig(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	var req SaveBackupConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	result, err := h.backupService.TestConfig(r.Context(), user.Username, &req)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, result)
}

func (h *Handler) GetBackupRcloneStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.backupService.GetRcloneStatus(r.Context())
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, status)
}

func (h *Handler) InstallBackupRclone(w http.ResponseWriter, r *http.Request) {
	status, err := h.backupService.InstallRclone(r.Context())
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusAccepted, status)
}

func (h *Handler) RunBackupNow(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	resp, err := h.backupService.TriggerManualBackup(r.Context(), user.Username)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusAccepted, resp)
}

func (h *Handler) ListBackupJobs(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	jobs, err := h.backupService.ListJobs(r.Context(), user.Username, limit)
	if err != nil {
		h.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, jobs)
}

func (h *Handler) ListBackupSnapshots(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	snapshots, err := h.backupService.ListSnapshots(r.Context(), user.Username, limit)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, snapshots)
}

func (h *Handler) DeleteBackupSnapshot(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	var req DeleteSnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.SnapshotID) == "" {
		h.error(w, http.StatusBadRequest, "snapshot_id is required")
		return
	}
	jobID, err := h.backupService.DeleteSnapshot(r.Context(), user.Username, strings.TrimSpace(req.SnapshotID))
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusAccepted, &BackupRunResponse{
		JobID:   jobID,
		Status:  "running",
		Message: "Snapshot delete started.",
	})
}

func (h *Handler) DownloadBackupSnapshot(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	snapshotID := strings.TrimSpace(chi.URLParam(r, "snapshotId"))
	if snapshotID == "" {
		h.error(w, http.StatusBadRequest, "snapshotId is required")
		return
	}
	zipPath, fileName, err := h.backupService.CreateSnapshotZip(r.Context(), user.Username, snapshotID)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	defer os.RemoveAll(filepath.Dir(zipPath))

	file, err := os.Open(zipPath)
	if err != nil {
		h.error(w, http.StatusInternalServerError, "failed to open snapshot archive")
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		h.error(w, http.StatusInternalServerError, "failed to stat snapshot archive")
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+fileName+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	http.ServeContent(w, r, fileName, info.ModTime(), file)
}

func (h *Handler) RestoreSite(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	var req RestoreSiteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.SnapshotID) == "" || strings.TrimSpace(req.SiteID) == "" {
		h.error(w, http.StatusBadRequest, "snapshot_id and site_id are required")
		return
	}
	jobID, err := h.backupService.RestoreSite(r.Context(), user.Username, &req)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusAccepted, &BackupRunResponse{
		JobID:   jobID,
		Status:  "running",
		Message: "Website restore started.",
	})
}

func (h *Handler) RestoreDatabase(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	var req RestoreDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.SnapshotID) == "" || strings.TrimSpace(req.DatabaseID) == "" {
		h.error(w, http.StatusBadRequest, "snapshot_id and database_id are required")
		return
	}
	jobID, err := h.backupService.RestoreDatabase(r.Context(), user.Username, &req)
	if err != nil {
		h.error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusAccepted, &BackupRunResponse{
		JobID:   jobID,
		Status:  "running",
		Message: "Database restore started.",
	})
}
