package main

import (
	"strings"
	"testing"
)

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
