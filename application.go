package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/jung-kurt/gofpdf"
)

type Applications struct {
	Idea       string   `json:"idea"`
	Leader     string   `json:"leader"`
	Email      string   `json:"email"`
	Phone      string   `json:"phone"`
	Department string   `json:"department"`
	Teams      []string `json:"teams"`
	Track      string   `json:"track"`
	Sector     string   `json:"sector"`
}

type Response struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, response Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}

func Health(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeJSON(w, http.StatusNotFound, Response{
			Status:  "error",
			Message: "route not found",
		})
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Status:  "success",
		Message: "Startup server is running",
	})
}

func Application(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)

		writeJSON(w, http.StatusMethodNotAllowed, Response{
			Status:  "error",
			Message: "method not allowed",
		})
		return
	}

	defer r.Body.Close()

	var app Applications

	err := json.NewDecoder(r.Body).Decode(&app)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, Response{
			Status:  "error",
			Message: "invalid JSON",
		})
		return
	}

	// Validate required fields
	if strings.TrimSpace(app.Idea) == "" ||
		strings.TrimSpace(app.Leader) == "" ||
		strings.TrimSpace(app.Email) == "" ||
		strings.TrimSpace(app.Phone) == "" ||
		strings.TrimSpace(app.Department) == "" ||
		strings.TrimSpace(app.Track) == "" ||
		strings.TrimSpace(app.Sector) == "" {

		writeJSON(w, http.StatusBadRequest, Response{
			Status:  "error",
			Message: "all application fields are required",
		})
		return
	}

	// Generate PDF
	pdfPath, err := GeneratePDF(app)
	if err != nil {
		log.Printf("generate PDF: %v", err)

		writeJSON(w, http.StatusInternalServerError, Response{
			Status:  "error",
			Message: "failed to generate application PDF",
		})
		return
	}

	// Delete temporary PDF after request finishes
	defer os.Remove(pdfPath)

	// Send email
	err = SendEmail(app, pdfPath)

	if err != nil {

		if isEmailConfigurationError(err) {
			log.Printf("email configuration error: %v", err)

			writeJSON(w, http.StatusInternalServerError, Response{
				Status:  "error",
				Message: "email service is not configured on the server",
			})
			return
		}

		log.Printf("send email: %v", err)

		writeJSON(w, http.StatusInternalServerError, Response{
			Status:  "error",
			Message: "failed to send email",
		})
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Status:  "success",
		Message: "application submitted successfully",
	})
}

func GeneratePDF(app Applications) (string, error) {

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()

	// =========================
	// HEADER
	// =========================

	pdf.SetFillColor(41, 128, 185)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Arial", "B", 20)

	pdf.CellFormat(
		190,
		15,
		"STARTUP APPLICATION",
		"",
		1,
		"C",
		true,
		0,
		"",
	)

	pdf.Ln(8)

	// =========================
	// APPLICANT DETAILS
	// =========================

	pdf.SetTextColor(0, 0, 0)
	pdf.SetFillColor(230, 240, 255)

	pdf.SetFont("Arial", "B", 14)

	pdf.CellFormat(
		190,
		10,
		"Applicant Details",
		"",
		1,
		"L",
		true,
		0,
		"",
	)

	pdf.SetFont("Arial", "", 12)

	pdf.Cell(45, 8, "Leader")
	pdf.Cell(0, 8, app.Leader)
	pdf.Ln(8)

	pdf.Cell(45, 8, "Email")
	pdf.Cell(0, 8, app.Email)
	pdf.Ln(8)

	pdf.Cell(45, 8, "Phone")
	pdf.Cell(0, 8, app.Phone)
	pdf.Ln(8)

	pdf.Cell(45, 8, "Department")
	pdf.Cell(0, 8, app.Department)
	pdf.Ln(12)

	// =========================
	// STARTUP DETAILS
	// =========================

	pdf.SetFillColor(230, 240, 255)
	pdf.SetFont("Arial", "B", 14)

	pdf.CellFormat(
		190,
		10,
		"Startup Details",
		"",
		1,
		"L",
		true,
		0,
		"",
	)

	pdf.SetFont("Arial", "", 12)

	pdf.Cell(45, 8, "Idea")

	// MultiCell allows long ideas
	pdf.MultiCell(
		145,
		8,
		app.Idea,
		"",
		"L",
		false,
	)

	pdf.Ln(2)

	pdf.Cell(45, 8, "Track")
	pdf.Cell(0, 8, app.Track)
	pdf.Ln(8)

	pdf.Cell(45, 8, "Sector")
	pdf.Cell(0, 8, app.Sector)
	pdf.Ln(12)

	// =========================
	// TEAM MEMBERS
	// =========================

	pdf.SetFillColor(230, 240, 255)
	pdf.SetFont("Arial", "B", 14)

	pdf.CellFormat(
		190,
		10,
		"Team Members",
		"",
		1,
		"L",
		true,
		0,
		"",
	)

	pdf.SetFont("Arial", "", 12)

	if len(app.Teams) == 0 {

		pdf.Cell(0, 8, "No additional team members")
		pdf.Ln(8)

	} else {

		for _, member := range app.Teams {

			member = strings.TrimSpace(member)

			if member == "" {
				continue
			}

			pdf.Cell(10, 8, "-")
			pdf.Cell(0, 8, member)
			pdf.Ln(8)
		}
	}

	// =========================
	// FOOTER
	// =========================

	pdf.SetY(-20)

	pdf.SetFont("Arial", "I", 10)
	pdf.SetTextColor(120, 120, 120)

	pdf.CellFormat(
		190,
		10,
		"Generated by Startup Application Portal",
		"",
		0,
		"C",
		false,
		0,
		"",
	)

	// =========================
	// CREATE TEMP PDF
	// =========================

	pdfFile, err := os.CreateTemp("", "startup-application-*.pdf")

	if err != nil {
		return "", fmt.Errorf("create PDF file: %w", err)
	}

	pdfPath := pdfFile.Name()

	if err := pdf.Output(pdfFile); err != nil {
		pdfFile.Close()
		os.Remove(pdfPath)

		return "", fmt.Errorf("write PDF: %w", err)
	}

	if err := pdfFile.Close(); err != nil {
		os.Remove(pdfPath)

		return "", fmt.Errorf("close PDF: %w", err)
	}

	return pdfPath, nil
}
func SendEmail(app Applications, pdfPath string) error {
	apiKey := os.Getenv("RESEND_API_KEY")
	admin := os.Getenv("ADMIN_EMAIL")

	// If email is not configured, skip sending in local/dev environments
	if strings.TrimSpace(apiKey) == "" || strings.TrimSpace(admin) == "" {
		log.Printf("email not configured (RESEND_API_KEY or ADMIN_EMAIL missing); skipping send in dev")
		return nil
	}

	// Debug: log whether env vars are present (do not print actual API key)
	log.Printf("SendEmail: apiKey set=%v, admin set=%v", strings.TrimSpace(apiKey) != "", strings.TrimSpace(admin) != "")

	pdfBytes, err := os.ReadFile(pdfPath)
	if err != nil {
		return fmt.Errorf("read PDF: %w", err)
	}

	body := fmt.Sprintf(
		"A new startup application has been submitted.\n\nLeader: %s\nEmail: %s\nPhone: %s\nIdea: %s",
		app.Leader, app.Email, app.Phone, app.Idea,
	)

	payload := map[string]any{
		"from":    "Startup Portal <onboarding@resend.dev>",
		"to":      []string{admin},
		"subject": "New Startup Application",
		"text":    body,
		"attachments": []map[string]string{
			{"filename": "application.pdf", "content": base64.StdEncoding.EncodeToString(pdfBytes)},
		},
	}

	reqBody, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		bodyStr := string(b)

		// Treat Resend "testing emails" 403 validation error as non-fatal in dev
		if resp.StatusCode == 403 && strings.Contains(bodyStr, "testing emails") {
			log.Printf("resend returned testing-mode restriction, skipping email send: %s", bodyStr)
			return nil
		}

		return fmt.Errorf("resend error (%d): %s", resp.StatusCode, bodyStr)
	}
	return nil
}

func isEmailConfigurationError(err error) bool {

	if err == nil {
		return false
	}

	return strings.Contains(
		err.Error(),
		"email configuration is incomplete",
	)
}
