package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rehmatworks/fastcp/internal/middleware"
	"github.com/rehmatworks/fastcp/internal/models"
	"github.com/rehmatworks/fastcp/internal/sites"
)

// FileInfo represents file/directory information
type FileInfo struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	IsDir   bool      `json:"is_dir"`
	ModTime time.Time `json:"mod_time"`
	Perm    string    `json:"perm"`
}

// FileListResponse represents a directory listing response
type FileListResponse struct {
	Path     string     `json:"path"`
	Files    []FileInfo `json:"files"`
	CanRead  bool       `json:"can_read"`
	CanWrite bool       `json:"can_write"`
}

// FileContent represents file content for editing
type FileContent struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Size    int64  `json:"size"`
	CanEdit bool   `json:"can_edit"`
}

// FileUploadRequest represents a file upload request
type FileUploadRequest struct {
	Path string `json:"path"`
}

// FileOperationRequest represents file operation requests
type FileOperationRequest struct {
	Path     string `json:"path"`
	NewPath  string `json:"new_path,omitempty"`
	Content  string `json:"content,omitempty"`
	DirName  string `json:"dir_name,omitempty"`
	FileName string `json:"file_name,omitempty"`
}

// FileManager provides secure file operations for sites
type FileManager struct {
	siteManager *sites.Manager
}

// NewFileManager creates a new file manager
func NewFileManager(siteManager *sites.Manager) *FileManager {
	return &FileManager{
		siteManager: siteManager,
	}
}

// validateSiteAccess checks if user has access to the site
func (fm *FileManager) validateSiteAccess(userID, siteID string) (*models.Site, error) {
	site, err := fm.siteManager.Get(siteID)
	if err != nil {
		return nil, err
	}

	// Admin can access all sites, users can only access their own
	if userID != "admin" && site.UserID != userID {
		return nil, fmt.Errorf("access denied")
	}

	return site, nil
}

// resolvePath resolves a relative path within the site's root directory
func (fm *FileManager) resolvePath(site *models.Site, relativePath string) (string, error) {
	// Clean and normalize the path
	cleanPath := filepath.Clean(relativePath)

	// Prevent directory traversal attacks
	if strings.Contains(cleanPath, "..") {
		return "", fmt.Errorf("invalid path: directory traversal not allowed")
	}

	// Resolve to absolute path within site root
	absPath := filepath.Join(site.RootPath, cleanPath)

	// Ensure the resolved path is still within the site root
	rel, err := filepath.Rel(site.RootPath, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid path: outside site directory")
	}

	return absPath, nil
}

// checkPermissions checks read/write permissions for a path
func (fm *FileManager) checkPermissions(path string) (canRead, canWrite bool) {
	// Check if path exists and is accessible
	_, err := os.Stat(path)
	if err != nil {
		return false, false
	}

	// For now, allow read/write access to all files within site directories
	// In production, you might want more granular permissions
	canRead = true
	canWrite = true

	// Special restrictions for sensitive files
	baseName := filepath.Base(path)
	if baseName == ".htaccess" || baseName == ".env" || strings.HasPrefix(baseName, ".") {
		// Allow read but restrict write for hidden files
		canWrite = false
	}

	return canRead, canWrite
}

// listFiles lists files in a directory
func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	path := r.URL.Query().Get("path")

	if path == "" {
		path = "." // Root directory
	}

	claims := middleware.GetClaims(r)

	// Validate site access
	site, err := s.fileManager.validateSiteAccess(claims.UserID, siteID)
	if err != nil {
		s.error(w, http.StatusForbidden, "access denied")
		return
	}

	// Resolve the path
	absPath, err := s.fileManager.resolvePath(site, path)
	if err != nil {
		s.error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check permissions
	canRead, canWrite := s.fileManager.checkPermissions(absPath)
	if !canRead {
		s.error(w, http.StatusForbidden, "read access denied")
		return
	}

	// Read directory
	entries, err := os.ReadDir(absPath)
	if err != nil {
		s.error(w, http.StatusInternalServerError, "failed to read directory")
		return
	}

	files := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue // Skip files we can't stat
		}

		fileInfo := FileInfo{
			Name:    entry.Name(),
			Path:    filepath.Join(path, entry.Name()),
			Size:    info.Size(),
			IsDir:   entry.IsDir(),
			ModTime: info.ModTime(),
			Perm:    info.Mode().String(),
		}
		files = append(files, fileInfo)
	}

	response := FileListResponse{
		Path:     path,
		Files:    files,
		CanRead:  canRead,
		CanWrite: canWrite,
	}

	s.success(w, response)
}

// getFileContent gets the content of a file for editing
func (s *Server) getFileContent(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	path := r.URL.Query().Get("path")

	if path == "" {
		s.error(w, http.StatusBadRequest, "path parameter required")
		return
	}

	claims := middleware.GetClaims(r)

	// Validate site access
	site, err := s.fileManager.validateSiteAccess(claims.UserID, siteID)
	if err != nil {
		s.error(w, http.StatusForbidden, "access denied")
		return
	}

	// Resolve the path
	absPath, err := s.fileManager.resolvePath(site, path)
	if err != nil {
		s.error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check permissions
	canRead, _ := s.fileManager.checkPermissions(absPath)
	if !canRead {
		s.error(w, http.StatusForbidden, "read access denied")
		return
	}

	// Check if it's a file
	info, err := os.Stat(absPath)
	if err != nil {
		s.error(w, http.StatusNotFound, "file not found")
		return
	}
	if info.IsDir() {
		s.error(w, http.StatusBadRequest, "path is a directory")
		return
	}

	// Check file size (limit to 1MB for editing)
	if info.Size() > 1024*1024 {
		s.error(w, http.StatusBadRequest, "file too large for editing")
		return
	}

	// Check if file is text-based
	content, err := os.ReadFile(absPath)
	if err != nil {
		s.error(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	// Basic check for binary files
	isText := true
	for _, b := range content {
		if b == 0 {
			isText = false
			break
		}
	}

	if !isText {
		s.error(w, http.StatusBadRequest, "binary files cannot be edited")
		return
	}

	canEdit := true
	baseName := filepath.Base(absPath)
	if baseName == ".htaccess" || baseName == ".env" || strings.HasPrefix(baseName, ".") {
		canEdit = false
	}

	response := FileContent{
		Path:    path,
		Content: string(content),
		Size:    info.Size(),
		CanEdit: canEdit,
	}

	s.success(w, response)
}

// saveFileContent saves file content
func (s *Server) saveFileContent(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")

	var req FileOperationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Path == "" || req.Content == "" {
		s.error(w, http.StatusBadRequest, "path and content are required")
		return
	}

	claims := middleware.GetClaims(r)

	// Validate site access
	site, err := s.fileManager.validateSiteAccess(claims.UserID, siteID)
	if err != nil {
		s.error(w, http.StatusForbidden, "access denied")
		return
	}

	// Resolve the path
	absPath, err := s.fileManager.resolvePath(site, req.Path)
	if err != nil {
		s.error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check permissions
	_, canWrite := s.fileManager.checkPermissions(absPath)
	if !canWrite {
		s.error(w, http.StatusForbidden, "write access denied")
		return
	}

	// Check if file exists and is not too large
	info, err := os.Stat(absPath)
	if err != nil {
		s.error(w, http.StatusNotFound, "file not found")
		return
	}
	if info.IsDir() {
		s.error(w, http.StatusBadRequest, "path is a directory")
		return
	}

	// Write the file
	err = os.WriteFile(absPath, []byte(req.Content), info.Mode())
	if err != nil {
		s.error(w, http.StatusInternalServerError, "failed to save file")
		return
	}

	s.success(w, map[string]string{"message": "file saved successfully"})
}

// createDirectory creates a new directory
func (s *Server) createDirectory(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")

	var req FileOperationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Path == "" || req.DirName == "" {
		s.error(w, http.StatusBadRequest, "path and dir_name are required")
		return
	}

	claims := middleware.GetClaims(r)

	// Validate site access
	site, err := s.fileManager.validateSiteAccess(claims.UserID, siteID)
	if err != nil {
		s.error(w, http.StatusForbidden, "access denied")
		return
	}

	// Resolve the parent path
	parentPath, err := s.fileManager.resolvePath(site, req.Path)
	if err != nil {
		s.error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check parent permissions
	_, canWrite := s.fileManager.checkPermissions(parentPath)
	if !canWrite {
		s.error(w, http.StatusForbidden, "write access denied")
		return
	}

	// Create the directory
	newDirPath := filepath.Join(parentPath, req.DirName)
	err = os.MkdirAll(newDirPath, 0755)
	if err != nil {
		s.error(w, http.StatusInternalServerError, "failed to create directory")
		return
	}

	s.success(w, map[string]string{"message": "directory created successfully"})
}

// deleteFile deletes a file or directory
func (s *Server) deleteFile(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")

	var req FileOperationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Path == "" {
		s.error(w, http.StatusBadRequest, "path is required")
		return
	}

	claims := middleware.GetClaims(r)

	// Validate site access
	site, err := s.fileManager.validateSiteAccess(claims.UserID, siteID)
	if err != nil {
		s.error(w, http.StatusForbidden, "access denied")
		return
	}

	// Resolve the path
	absPath, err := s.fileManager.resolvePath(site, req.Path)
	if err != nil {
		s.error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check permissions
	_, canWrite := s.fileManager.checkPermissions(absPath)
	if !canWrite {
		s.error(w, http.StatusForbidden, "write access denied")
		return
	}

	// Check if path exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		s.error(w, http.StatusNotFound, "file not found")
		return
	}

	// Delete the file/directory
	err = os.RemoveAll(absPath)
	if err != nil {
		s.error(w, http.StatusInternalServerError, "failed to delete")
		return
	}

	s.success(w, map[string]string{"message": "deleted successfully"})
}

// uploadFile handles file uploads
func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	path := r.URL.Query().Get("path")

	if path == "" {
		path = "." // Root directory
	}

	claims := middleware.GetClaims(r)

	// Validate site access
	site, err := s.fileManager.validateSiteAccess(claims.UserID, siteID)
	if err != nil {
		s.error(w, http.StatusForbidden, "access denied")
		return
	}

	// Resolve the path
	absPath, err := s.fileManager.resolvePath(site, path)
	if err != nil {
		s.error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check permissions
	_, canWrite := s.fileManager.checkPermissions(absPath)
	if !canWrite {
		s.error(w, http.StatusForbidden, "write access denied")
		return
	}

	// Parse multipart form
	err = r.ParseMultipartForm(32 << 20) // 32MB max
	if err != nil {
		s.error(w, http.StatusBadRequest, "failed to parse multipart form")
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		s.error(w, http.StatusBadRequest, "no files uploaded")
		return
	}

	uploaded := 0
	for _, fileHeader := range files {
		file, err := fileHeader.Open()
		if err != nil {
			continue
		}
		defer file.Close()

		// Create destination file
		destPath := filepath.Join(absPath, fileHeader.Filename)
		destFile, err := os.Create(destPath)
		if err != nil {
			continue
		}
		defer destFile.Close()

		// Copy file content
		_, err = io.Copy(destFile, file)
		if err != nil {
			continue
		}

		uploaded++
	}

	s.success(w, map[string]interface{}{
		"message":  "files uploaded successfully",
		"uploaded": uploaded,
	})
}

// downloadFile handles file downloads
func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	path := r.URL.Query().Get("path")

	if path == "" {
		s.error(w, http.StatusBadRequest, "path parameter required")
		return
	}

	claims := middleware.GetClaims(r)

	// Validate site access
	site, err := s.fileManager.validateSiteAccess(claims.UserID, siteID)
	if err != nil {
		s.error(w, http.StatusForbidden, "access denied")
		return
	}

	// Resolve the path
	absPath, err := s.fileManager.resolvePath(site, path)
	if err != nil {
		s.error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check permissions
	canRead, _ := s.fileManager.checkPermissions(absPath)
	if !canRead {
		s.error(w, http.StatusForbidden, "read access denied")
		return
	}

	// Check if it's a file
	info, err := os.Stat(absPath)
	if err != nil {
		s.error(w, http.StatusNotFound, "file not found")
		return
	}
	if info.IsDir() {
		s.error(w, http.StatusBadRequest, "cannot download directory")
		return
	}

	// Set headers for download
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filepath.Base(absPath)))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))

	// Stream the file
	file, err := os.Open(absPath)
	if err != nil {
		s.error(w, http.StatusInternalServerError, "failed to open file")
		return
	}
	defer file.Close()

	io.Copy(w, file)
}
