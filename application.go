package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	// Using Apps Script webhook instead of direct Google APIs
)

type Applications struct {
	Idea        string   `json:"idea"`
	Leader      string   `json:"leader"`
	Email       string   `json:"email"`
	Phone       string   `json:"phone"`
	Department  string   `json:"department"`
	Teams       []string `json:"teams"`
	Track       string   `json:"track"`
	Sector      string   `json:"sector"`
	Description string   `json:"description"`
	SubmittedAt string   `json:"submittedAt,omitempty"`
	PDFLink     string   `json:"pdfLink,omitempty"`
}

type apiResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

const (
	maxUploadSize = 10 << 20 // 10 MB
	maxMemory     = 8 << 20  // 8 MB in memory for multipart form parsing
)

func writeJSON(w http.ResponseWriter, status int, response apiResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}

func Health(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeJSON(w, http.StatusNotFound, apiResponse{Success: false, Message: "route not found"})
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "Startup server is running"})
}

func Application(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Message: "method not allowed"})
		return
	}

	log.Printf("Received application request from %s, method=%s", r.RemoteAddr, r.Method)

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxMemory); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			log.Printf("parse multipart form failed: %v", err)
			writeJSON(w, http.StatusRequestEntityTooLarge, apiResponse{Success: false, Message: "uploaded file is too large"})
			return
		}
		log.Printf("parse multipart form failed: %v", err)

		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Message: "failed to parse multipart form data"})
		return
	}

	log.Printf("Parsed multipart form data")

	app, pdfBytes, err := parseApplicationRequest(r.MultipartForm)
	if err != nil {
		log.Printf("parse application request failed: %v", err)
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Message: err.Error()})
		return
	}

	log.Printf("Application data collected: Idea=%q Leader=%q Teams=%d", app.Idea, app.Leader, len(app.Teams))

	if err := validateApplication(app); err != nil {
		log.Printf("application validation failed: %v", err)
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Message: err.Error()})
		return
	}

	app.SubmittedAt = time.Now().UTC().Format(time.RFC3339)

	log.Printf("Starting upload to Google Drive")
	if err := uploadApplicationToDrive(r.Context(), app, pdfBytes); err != nil {
		log.Printf("upload to Google Drive failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Message: "failed to save application to Google Drive"})
		return
	}

	log.Printf("Application successfully saved to Google Drive")
	writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "Application submitted and saved to Google Drive"})
}

func parseApplicationRequest(form *multipart.Form) (Applications, []byte, error) {
	getValue := func(key string) string {
		if values, ok := form.Value[key]; ok && len(values) > 0 {
			return strings.TrimSpace(values[0])
		}
		return ""
	}

	app := Applications{
		Idea:        getValue("idea"),
		Leader:      getValue("leader"),
		Email:       getValue("email"),
		Phone:       getValue("phone"),
		Department:  getValue("department"),
		Track:       getValue("track"),
		Sector:      getValue("sector"),
		Description: getValue("description"),
	}

	for _, team := range form.Value["teams"] {
		trimmed := strings.TrimSpace(team)
		if trimmed != "" {
			app.Teams = append(app.Teams, trimmed)
		}
	}

	files := form.File["applicationPDF"]
	if len(files) == 0 {
		return Applications{}, nil, errors.New("application PDF file is required")
	}

	fileHeader := files[0]
	if fileHeader.Size > maxUploadSize {
		return Applications{}, nil, errors.New("uploaded PDF exceeds size limit")
	}

	if !isPDFFile(fileHeader) {
		return Applications{}, nil, errors.New("uploaded file must be a PDF")
	}

	file, err := fileHeader.Open()
	if err != nil {
		return Applications{}, nil, fmt.Errorf("open uploaded PDF: %w", err)
	}
	defer file.Close()

	pdfBytes, err := io.ReadAll(file)
	if err != nil {
		return Applications{}, nil, fmt.Errorf("read uploaded PDF: %w", err)
	}

	if len(pdfBytes) == 0 {
		return Applications{}, nil, errors.New("uploaded PDF file is empty")
	}

	if len(pdfBytes) > maxUploadSize {
		return Applications{}, nil, errors.New("uploaded PDF exceeds size limit")
	}

	return app, pdfBytes, nil
}

func isPDFFile(header *multipart.FileHeader) bool {
	contentType := strings.ToLower(header.Header.Get("Content-Type"))
	if strings.Contains(contentType, "pdf") {
		return true
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	return ext == ".pdf"
}

func validateApplication(app Applications) error {
	if strings.TrimSpace(app.Idea) == "" ||
		strings.TrimSpace(app.Leader) == "" ||
		strings.TrimSpace(app.Email) == "" ||
		strings.TrimSpace(app.Phone) == "" ||
		strings.TrimSpace(app.Department) == "" ||
		strings.TrimSpace(app.Track) == "" ||
		strings.TrimSpace(app.Sector) == "" ||
		strings.TrimSpace(app.Description) == "" ||
		len(app.Teams) == 0 {
		return fmt.Errorf("all application fields are required")
	}

	for _, member := range app.Teams {
		if strings.TrimSpace(member) == "" {
			return fmt.Errorf("team member names cannot be empty")
		}
	}

	if wordCount(app.Description) > 150 {
		return fmt.Errorf("description must be 150 words or fewer")
	}

	return nil
}

func uploadApplicationToDrive(ctx context.Context, app Applications, pdfBytes []byte) error {
	appsScriptURL := strings.TrimSpace(os.Getenv("GOOGLE_APPS_SCRIPT_URL"))
	if appsScriptURL == "" {
		return errors.New("Apps Script URL is not configured: set GOOGLE_APPS_SCRIPT_URL")
	}

	log.Printf("Preparing payload for Apps Script at %s", appsScriptURL)

	// Build sanitized folder name using team leader and timestamp
	leader := strings.TrimSpace(app.Leader)
	sanitized := sanitizeFolderName(leader)
	// Use submitted time if available, else now
	ts := time.Now().UTC()
	if app.SubmittedAt != "" {
		if t, err := time.Parse(time.RFC3339, app.SubmittedAt); err == nil {
			ts = t.UTC()
		}
	}
	timeStamp := ts.Format("2006-01-02_1504") // e.g. 2026-08-11_1030
	folderName := fmt.Sprintf("%s_%s", sanitized, timeStamp)
	log.Printf("Using folder name: %s", folderName)

	// Encode PDF to base64
	log.Printf("Encoding PDF to base64 (size=%d bytes)", len(pdfBytes))
	b64 := base64.StdEncoding.EncodeToString(pdfBytes)

	payload := map[string]interface{}{
		"application": app,
		"folderName":  folderName,
		"pdf": map[string]string{
			"filename": "application.pdf",
			"base64":   b64,
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("marshal payload failed: %v", err)
		return fmt.Errorf("marshal payload: %w", err)
	}

	log.Printf("Sending application data to Apps Script (payload bytes=%d)", len(bodyBytes))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, appsScriptURL, bytes.NewReader(bodyBytes))
	if err != nil {
		log.Printf("create request to Apps Script failed: %v", err)
		return fmt.Errorf("create request to Apps Script: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("request to Apps Script failed: %v", err)
		return fmt.Errorf("request to Apps Script: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("Apps Script responded with status=%d body=%s", resp.StatusCode, string(respBody))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("apps script returned status %d", resp.StatusCode)
	}

	var asResp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(respBody, &asResp); err != nil {
		log.Printf("failed to parse Apps Script response: %v", err)
		return fmt.Errorf("invalid response from Apps Script: %w", err)
	}

	if !asResp.Success {
		log.Printf("Apps Script reported failure: %s", asResp.Message)
		return fmt.Errorf("apps script error: %s", asResp.Message)
	}

	log.Printf("Apps Script saved application: %s", asResp.Message)
	return nil
}

func wordCount(value string) int {
	return len(strings.Fields(value))
}

// sanitizeFolderName replaces characters not allowed or problematic in Drive folder names
func sanitizeFolderName(name string) string {
	if name == "" {
		return "Application"
	}
	// Replace any character that is not alphanumeric, space, hyphen or underscore with underscore
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	s := strings.TrimSpace(b.String())
	// Replace spaces with underscores
	s = strings.ReplaceAll(s, " ", "_")
	if s == "" {
		return "Application"
	}
	// limit length
	if len(s) > 100 {
		return s[:100]
	}
	return s
}
