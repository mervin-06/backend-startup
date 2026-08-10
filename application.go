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

	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
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

	app.SubmittedAt = time.Now().UTC().Format(time.RFC3339)

	if err := uploadApplicationToDrive(r.Context(), app, pdfBytes); err != nil {
		log.Printf("upload to Google Drive: %v", err)
		writeJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Message: "failed to save application to Google Drive"})
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "Application submitted successfully"})
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
	driveFolderID := strings.TrimSpace(os.Getenv("GOOGLE_DRIVE_FOLDER_ID"))
	if driveFolderID == "" {
		return errors.New("Google Drive configuration is incomplete: GOOGLE_DRIVE_FOLDER_ID must be set")
	}

	driveService, err := newDriveService(ctx)
	if err != nil {
		return fmt.Errorf("create Google Drive service: %w", err)
	}

	folderName, err := nextApplicationFolderName(ctx, driveService, driveFolderID)
	if err != nil {
		return fmt.Errorf("determine application folder name: %w", err)
	}

	appFolder, err := createDriveFolder(ctx, driveService, folderName, driveFolderID)
	if err != nil {
		return fmt.Errorf("create application folder: %w", err)
	}

	pdfFile, err := uploadDriveFile(ctx, driveService, "application.pdf", "application/pdf", appFolder.Id, bytes.NewReader(pdfBytes))
	if err != nil {
		return fmt.Errorf("upload PDF to Drive: %w", err)
	}

	app.PDFLink = pdfFile.WebViewLink

	jsonBytes, err := json.MarshalIndent(app, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal application JSON: %w", err)
	}

	if _, err := uploadDriveFile(ctx, driveService, "application.json", "application/json", appFolder.Id, bytes.NewReader(jsonBytes)); err != nil {
		return fmt.Errorf("upload application JSON to Drive: %w", err)
	}

	sheetID := strings.TrimSpace(os.Getenv("GOOGLE_SHEET_ID"))
	if sheetID != "" {
		sheetsService, err := newSheetsService(ctx)
		if err != nil {
			return fmt.Errorf("create Google Sheets service: %w", err)
		}

		if err := appendApplicationRow(ctx, sheetsService, sheetID, app); err != nil {
			return fmt.Errorf("append application row to Google Sheets: %w", err)
		}
	}

	return nil
}

func newGoogleClient(ctx context.Context) (*http.Client, error) {
	serviceAccountJSON, err := getServiceAccountJSON()
	if err != nil {
		return nil, err
	}

	config, err := google.JWTConfigFromJSON(serviceAccountJSON,
		drive.DriveFileScope,
		drive.DriveMetadataScope,
		sheets.SpreadsheetsScope,
	)
	if err != nil {
		return nil, fmt.Errorf("create service account credentials: %w", err)
	}

	return config.Client(ctx), nil
}

func getServiceAccountJSON() ([]byte, error) {
	if jsonData := strings.TrimSpace(os.Getenv("GOOGLE_SERVICE_ACCOUNT_JSON")); jsonData != "" {
		return []byte(jsonData), nil
	}

	if encoded := strings.TrimSpace(os.Getenv("GOOGLE_SERVICE_ACCOUNT_JSON_BASE64")); encoded != "" {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode GOOGLE_SERVICE_ACCOUNT_JSON_BASE64: %w", err)
		}
		return decoded, nil
	}

	return nil, errors.New("Google Drive configuration is incomplete: set GOOGLE_SERVICE_ACCOUNT_JSON or GOOGLE_SERVICE_ACCOUNT_JSON_BASE64")
}

func newDriveService(ctx context.Context) (*drive.Service, error) {
	client, err := newGoogleClient(ctx)
	if err != nil {
		return nil, err
	}

	return drive.NewService(ctx, option.WithHTTPClient(client))
}

func newSheetsService(ctx context.Context) (*sheets.Service, error) {
	client, err := newGoogleClient(ctx)
	if err != nil {
		return nil, err
	}

	return sheets.NewService(ctx, option.WithHTTPClient(client))
}

func nextApplicationFolderName(ctx context.Context, svc *drive.Service, parentFolderID string) (string, error) {
	query := fmt.Sprintf("mimeType = 'application/vnd.google-apps.folder' and '%s' in parents and trashed = false", parentFolderID)
	pageToken := ""
	count := 0

	for {
		resp, err := svc.Files.List().Q(query).
			Fields("nextPageToken, files(id)").
			PageToken(pageToken).
			SupportsAllDrives(true).
			Context(ctx).
			Do()
		if err != nil {
			return "", err
		}

		count += len(resp.Files)
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return fmt.Sprintf("Application_%03d", count+1), nil
}

func createDriveFolder(ctx context.Context, svc *drive.Service, folderName, parentFolderID string) (*drive.File, error) {
	folder := &drive.File{
		Name:     folderName,
		MimeType: "application/vnd.google-apps.folder",
		Parents:  []string{parentFolderID},
	}

	return svc.Files.Create(folder).
		Fields("id").
		SupportsAllDrives(true).
		Context(ctx).
		Do()
}

func uploadDriveFile(ctx context.Context, svc *drive.Service, name, mimeType, parentFolderID string, content io.Reader) (*drive.File, error) {
	driveFile := &drive.File{
		Name:    name,
		Parents: []string{parentFolderID},
	}

	file, err := svc.Files.Create(driveFile).
		Media(content).
		Fields("id, webViewLink").
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err != nil {
		return nil, err
	}

	return file, nil
}

func appendApplicationRow(ctx context.Context, svc *sheets.Service, spreadsheetID string, app Applications) error {
	sheetRange := strings.TrimSpace(os.Getenv("GOOGLE_SHEET_RANGE"))
	if sheetRange == "" {
		sheetRange = "Sheet1!A:I"
	}

	values := []interface{}{
		app.Leader,
		app.Email,
		app.Phone,
		app.Department,
		app.Idea,
		app.Track,
		app.Sector,
		app.PDFLink,
		app.SubmittedAt,
	}

	valueRange := &sheets.ValueRange{Values: [][]interface{}{values}}

	_, err := svc.Spreadsheets.Values.Append(spreadsheetID, sheetRange, valueRange).
		ValueInputOption("RAW").
		InsertDataOption("INSERT_ROWS").
		Context(ctx).
		Do()
	return err
}

func wordCount(value string) int {
	return len(strings.Fields(value))
}
