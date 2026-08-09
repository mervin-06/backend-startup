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
	"strconv"
	"strings"

	"github.com/jung-kurt/gofpdf"
	"gopkg.in/gomail.v2"
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

	if err := validateApplication(app); err != nil {
		writeJSON(w, http.StatusBadRequest, Response{
			Status:  "error",
			Message: err.Error(),
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

func validateApplication(app Applications) error {
	if strings.TrimSpace(app.Idea) == "" ||
		strings.TrimSpace(app.Leader) == "" ||
		strings.TrimSpace(app.Email) == "" ||
		strings.TrimSpace(app.Phone) == "" ||
		strings.TrimSpace(app.Department) == "" ||
		strings.TrimSpace(app.Track) == "" ||
		strings.TrimSpace(app.Sector) == "" ||
		strings.TrimSpace(app.Description) == "" ||
		len(app.Teams) != 3 {
		return fmt.Errorf("all application fields are required")
	}

	for _, member := range app.Teams {
		if strings.TrimSpace(member) == "" {
			return fmt.Errorf("three team member names are required")
		}
	}

	if wordCount(app.Description) > 150 {
		return fmt.Errorf("description must be 150 words or fewer")
	}

	return nil
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
	pdf.Ln(10)

	pdf.SetFont("Arial", "B", 12)
	pdf.Cell(45, 8, "Description")
	pdf.SetFont("Arial", "", 12)
	pdf.MultiCell(145, 8, app.Description, "", "L", false)
	pdf.Ln(6)

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

const resendSandboxFrom = "onboarding@resend.dev"

func resolveFromAddress(from string) string {
	from = strings.TrimSpace(from)
	if from == "" {
		return resendSandboxFrom
	}

	if strings.Contains(from, "<") || !strings.Contains(from, "@") {
		return resendSandboxFrom
	}

	return fmt.Sprintf("Startup Portal <%s>", from)
}

func resolveSMTPFromAddress(from, smtpUser string) string {
	from = strings.TrimSpace(from)
	if from != "" && strings.Contains(from, "@") {
		if strings.Contains(from, "<") {
			return from
		}
		return fmt.Sprintf("Startup Portal <%s>", from)
	}

	smtpUser = strings.TrimSpace(smtpUser)
	if smtpUser != "" && strings.Contains(smtpUser, "@") {
		return smtpUser
	}

	return ""
}

func parseRecipients(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\t' || r == ' '
	})

	var recipients []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		recipients = append(recipients, trimmed)
	}

	return recipients
}

func SendEmail(app Applications, pdfPath string) error {
	admin := strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))
	from := strings.TrimSpace(os.Getenv("FROM_EMAIL"))

	adminRecipients := parseRecipients(admin)
	if len(adminRecipients) == 0 {
		return fmt.Errorf("email configuration is incomplete: ADMIN_EMAIL must be set")
	}

	log.Printf("SendEmail: admin=%q, from=%q", adminRecipients, from)

	pdfBytes, err := os.ReadFile(pdfPath)
	if err != nil {
		return fmt.Errorf("read PDF: %w", err)
	}

	body := fmt.Sprintf(
		"A new startup application has been submitted.\n\nLeader: %s\nEmail: %s\nPhone: %s\nIdea: %s\nDescription: %s",
		app.Leader, app.Email, app.Phone, app.Idea, app.Description,
	)

	subject := "New Startup Application"

	apiKey := strings.TrimSpace(os.Getenv("RESEND_API_KEY"))
	smtpHost := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	smtpPort := strings.TrimSpace(os.Getenv("SMTP_PORT"))
	smtpUser := strings.TrimSpace(os.Getenv("SMTP_USER"))
	smtpPass := strings.TrimSpace(os.Getenv("SMTP_PASS"))
	smtpConfigured := smtpHost != "" && smtpPort != "" && smtpUser != "" && smtpPass != ""

	if apiKey != "" {
		err = sendEmailWithResend(apiKey, from, adminRecipients, subject, body, pdfBytes)
		if err == nil {
			return nil
		}

		if smtpConfigured {
			log.Printf("Resend failed, attempting SMTP fallback: %v", err)
			smtpErr := sendEmailWithSMTP(smtpHost, smtpPort, smtpUser, smtpPass, from, adminRecipients, subject, body, pdfPath)
			if smtpErr == nil {
				return nil
			}
			return fmt.Errorf("resend failed: %w; smtp fallback failed: %v", err, smtpErr)
		}

		return err
	}

	if smtpConfigured {
		return sendEmailWithSMTP(smtpHost, smtpPort, smtpUser, smtpPass, from, adminRecipients, subject, body, pdfPath)
	}

	return fmt.Errorf("email configuration is incomplete: RESEND_API_KEY or SMTP_HOST/SMTP_PORT/SMTP_USER/SMTP_PASS must be set")
}

func sendEmailWithResend(apiKey, from string, recipients []string, subject, body string, pdfBytes []byte) error {
	if from = resolveFromAddress(from); from == "" {
		from = resendSandboxFrom
	}

	payload := map[string]any{
		"from":    from,
		"to":      recipients,
		"subject": subject,
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

		if resp.StatusCode == 403 {
			log.Printf("resend 403 response: %s", bodyStr)
			if strings.Contains(bodyStr, "domain is not verified") || strings.Contains(bodyStr, "testing emails") {
				if from != resendSandboxFrom {
					log.Printf("retrying email with default resend sender %q", resendSandboxFrom)
					payload["from"] = resendSandboxFrom
					reqBody, _ = json.Marshal(payload)
					req, _ = http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(reqBody))
					req.Header.Set("Authorization", "Bearer "+apiKey)
					req.Header.Set("Content-Type", "application/json")

					resp, err = http.DefaultClient.Do(req)
					if err != nil {
						return fmt.Errorf("send request after fallback: %w", err)
					}
					defer resp.Body.Close()

					if resp.StatusCode >= 300 {
						b, _ = io.ReadAll(resp.Body)
						return fmt.Errorf("email configuration is incomplete: %s", string(b))
					}
					return nil
				}
				return fmt.Errorf("email configuration is incomplete: %s", bodyStr)
			}
			return fmt.Errorf("resend 403 forbidden: %s", bodyStr)
		}

		return fmt.Errorf("resend error (%d): %s", resp.StatusCode, bodyStr)
	}

	return nil
}

func sendEmailWithSMTP(host, port, user, pass, from string, recipients []string, subject, body string, pdfPath string) error {
	fromAddress := resolveSMTPFromAddress(from, user)
	if fromAddress == "" {
		fromAddress = "Startup Portal <no-reply@startup-portal.local>"
	}

	portNum, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("invalid SMTP_PORT: %w", err)
	}

	message := gomail.NewMessage()
	message.SetHeader("From", fromAddress)
	message.SetHeader("To", recipients...)
	message.SetHeader("Subject", subject)
	message.SetBody("text/plain", body)
	message.Attach(pdfPath)

	dialer := gomail.NewDialer(host, portNum, user, pass)
	if err := dialer.DialAndSend(message); err != nil {
		return fmt.Errorf("send email over SMTP: %w", err)
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
