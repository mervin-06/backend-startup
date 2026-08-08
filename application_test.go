package main

import (
	"strings"
	"testing"
)

func TestValidateApplicationRequiresStage2Fields(t *testing.T) {
	app := Applications{
		Idea:        "Smart app",
		Leader:      "Asha",
		Email:       "asha@example.com",
		Phone:       "0712345678",
		Department:  "CSE",
		Teams:       []string{"Ana", "Ben", "Cara"},
		Track:       "Technology & Innovation",
		Sector:      "Healthcare",
		Description: "A helpful platform for students.",
		InputOne:    "",
		InputTwo:    "A clear use case",
		InputThree:  "Strong growth plan",
	}

	err := validateApplication(app)
	if err == nil {
		t.Fatal("expected validation to fail when stage 2 fields are missing")
	}

	if !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected required-field error, got %v", err)
	}
}
