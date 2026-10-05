package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidAdminKey(t *testing.T) {
	const configured = "0123456789abcdef0123456789abcdef"
	t.Setenv("ADMIN_KEY", configured)

	if !validAdminKey(configured) {
		t.Fatal("configured admin key was rejected")
	}
	if validAdminKey("wrong-key") {
		t.Fatal("incorrect admin key was accepted")
	}

	t.Setenv("ADMIN_KEY", "short")
	if validAdminKey("short") {
		t.Fatal("short configured admin key was accepted")
	}
}

func TestAuthorizeAdmin(t *testing.T) {
	const configured = "0123456789abcdef0123456789abcdef"
	t.Setenv("ADMIN_KEY", configured)

	request := httptest.NewRequest(http.MethodPut, "/api/admin/portfolio", nil)
	request.Header.Set("Authorization", "Bearer "+configured)
	response := httptest.NewRecorder()
	if !authorizeAdmin(response, request) {
		t.Fatal("valid bearer key was rejected")
	}

	request = httptest.NewRequest(http.MethodPut, "/api/admin/portfolio", nil)
	response = httptest.NewRecorder()
	if authorizeAdmin(response, request) {
		t.Fatal("missing bearer key was accepted")
	}
	if response.Code != http.StatusUnauthorized {
		t.Errorf("missing-key response status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestValidatePortfolio(t *testing.T) {
	valid := portfolio{
		Profile: profile{
			Name: "Portfolio Owner", Role: "Developer", Headline: "A headline",
			About: "About me", Location: "Local",
		},
		Experiences: []experience{{
			Company: "Studio", Role: "Developer", StartDate: "2024-01-01",
		}},
		Skills:   []skill{{Name: "Go", Category: "Backend", Level: 4}},
		Projects: []project{{Title: "Portfolio", Year: 2025}},
	}
	if err := validatePortfolio(valid); err != nil {
		t.Fatalf("valid portfolio rejected: %v", err)
	}

	valid.Experiences[0].StartDate = "yesterday"
	if err := validatePortfolio(valid); err == nil {
		t.Fatal("invalid experience date was accepted")
	}
}
