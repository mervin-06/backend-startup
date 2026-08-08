package main

import (
	"strings"
	"testing"
)

func TestParseRecipientsSupportsCommaSeparatedEmails(t *testing.T) {
	recipients := parseRecipients("admin@example.com, team@example.com")

	if len(recipients) != 2 {
		t.Fatalf("expected 2 recipients, got %d", len(recipients))
	}

	if recipients[0] != "admin@example.com" || recipients[1] != "team@example.com" {
		t.Fatalf("unexpected recipients: %v", recipients)
	}
}

func TestResolveFromAddressFallsBackToResendSandbox(t *testing.T) {
	got := resolveFromAddress("")
	if got != resendSandboxFrom {
		t.Fatalf("expected fallback sender %q, got %q", resendSandboxFrom, got)
	}
}

func TestValidateApplicationRequiresDescriptionAndBaseFields(t *testing.T) {
	app := Applications{
		Idea:        "Smart app",
		Leader:      "Asha",
		Email:       "asha@example.com",
		Phone:       "0712345678",
		Department:  "CSE",
		Teams:       []string{"Ana", "Ben", "Cara"},
		Track:       "Technology & Innovation",
		Sector:      "Healthcare",
		Description: "",
	}

	err := validateApplication(app)
	if err == nil {
		t.Fatal("expected validation to fail when the description is missing")
	}

	if !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected required-field error, got %v", err)
	}
}
