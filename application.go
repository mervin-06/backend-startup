package main

import (
	"bytes"
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
}

type apiResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

const (
	maxUploadSize = 10 << 20 // 10 MB
	maxMemory     = 8 << 20  // 8 MB in memory for multipart form parsing
	resendAPIURL  = "https://api.resend.com/emails"
	defaultFrom   = "Startup Portal <onboarding@resend.dev>"
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

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxMemory); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeJSON(w, http.StatusRequestEntityTooLarge, apiResponse{Success: false, Message: "uploaded file is too large"})
			return
		}

		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Message: "failed to parse multipart form data"})
		return
	}

	app, pdfBytes, err := parseApplicationRequest(r.MultipartForm)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Message: err.Error()})
		return
	}

	if err := validateApplication(app); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Success: false, Message: err.Error()})
		return
	}

	if err := SendEmail(app, pdfBytes); err != nil {
		log.Printf("send email: %v", err)
		if isEmailConfigurationError(err) {
			writeJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Message: "email service is not configured on the server"})
			return
		}

		writeJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Message: "Application received but email could not be sent"})
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "Application submitted and email sent successfully"})
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

func SendEmail(app Applications, pdfBytes []byte) error {
	adminEmail := strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))
	if adminEmail == "" {
		return errors.New("email configuration is incomplete: ADMIN_EMAIL must be set")
	}

	apiKey := strings.TrimSpace(os.Getenv("RESEND_API_KEY"))
	if apiKey == "" {
		return errors.New("email configuration is incomplete: RESEND_API_KEY must be set")
	}

	from := strings.TrimSpace(os.Getenv("SENDER_EMAIL"))
	if from == "" || !strings.Contains(from, "@") {
		from = defaultFrom
	}

	body := fmt.Sprintf(
		"A new startup application has been submitted.\n\nVenture/Idea name: %s\nTeam leader: %s\nEmail: %s\nPhone: %s\nDepartment: %s\nTrack: %s\nSector: %s\n\nDescription:\n%s\n\nTeam members:\n%s",
		app.Idea,
		app.Leader,
		app.Email,
		app.Phone,
		app.Department,
		app.Track,
		app.Sector,
		app.Description,
		strings.Join(app.Teams, "\n"),
	)

	subject := fmt.Sprintf("New Startup Application - %s", app.Idea)
	return sendEmailWithResend(apiKey, from, adminEmail, subject, body, pdfBytes)
}

func sendEmailWithResend(apiKey, from, recipient, subject, body string, pdfBytes []byte) error {
	payload := map[string]any{
		"from":    from,
		"to":      []string{recipient},
		"subject": subject,
		"text":    body,
		"attachments": []map[string]string{
			{"filename": "startup_application.pdf", "content": base64.StdEncoding.EncodeToString(pdfBytes), "type": "application/pdf"},
		},
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal resend payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, resendAPIURL, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("create resend request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("resend error (%d): %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	return nil
}

func wordCount(value string) int {
	return len(strings.Fields(value))
}

func isEmailConfigurationError(err error) bool {
	if err == nil {
		return false
	}

	return strings.Contains(err.Error(), "email configuration is incomplete")
}
