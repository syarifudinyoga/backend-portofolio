package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestHandleAdminAuth(t *testing.T) {
	const configured = "0123456789abcdef0123456789abcdef"
	t.Setenv("ADMIN_KEY", configured)

	t.Run("valid key", func(t *testing.T) {
		body := fmt.Sprintf(`{"key": %q}`, configured)
		req := httptest.NewRequest(http.MethodPost, "/api/admin/auth", strings.NewReader(body))
		rec := httptest.NewRecorder()

		handleAdminAuth(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		var env encryptedEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		decrypted, err := decryptData(env.IV, env.Data)
		if err != nil {
			t.Fatalf("decrypt error: %v", err)
		}
		var res map[string]bool
		if err := json.Unmarshal(decrypted, &res); err != nil {
			t.Fatalf("unmarshal decrypted error: %v", err)
		}
		if !res["authenticated"] {
			t.Error("expected authenticated: true")
		}
	})

	t.Run("invalid key", func(t *testing.T) {
		body := `{"key": "wrong-key-value-12345"}`
		req := httptest.NewRequest(http.MethodPost, "/api/admin/auth", strings.NewReader(body))
		rec := httptest.NewRecorder()

		handleAdminAuth(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/auth", strings.NewReader(`{invalid-json`))
		rec := httptest.NewRecorder()

		handleAdminAuth(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})
}

func TestHandleSavePortfolio(t *testing.T) {
	logger := log.New(io.Discard, "", 0)

	t.Run("invalid json payload returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/admin/portfolio", strings.NewReader(`{not-json`))
		rec := httptest.NewRecorder()

		handleSavePortfolio(rec, req, nil, logger)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("validation error returns 422", func(t *testing.T) {
		invalidData := portfolio{
			Profile: profile{
				Name: "", // missing required name
			},
		}
		raw, _ := json.Marshal(invalidData)
		req := httptest.NewRequest(http.MethodPut, "/api/admin/portfolio", strings.NewReader(string(raw)))
		rec := httptest.NewRecorder()

		handleSavePortfolio(rec, req, nil, logger)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 StatusUnprocessableEntity, got %d", rec.Code)
		}
	})
}

func TestDecodeJSON(t *testing.T) {
	type targetType struct {
		Name string `json:"name"`
	}

	t.Run("valid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name": "test"}`))
		rec := httptest.NewRecorder()
		var target targetType
		if err := decodeJSON(rec, req, &target); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if target.Name != "test" {
			t.Errorf("expected name 'test', got %q", target.Name)
		}
	})

	t.Run("unknown fields rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name": "test", "extra": 123}`))
		rec := httptest.NewRecorder()
		var target targetType
		if err := decodeJSON(rec, req, &target); err == nil {
			t.Fatal("expected error on unknown field")
		}
	})

	t.Run("trailing json rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name": "test"} {"name": "second"}`))
		rec := httptest.NewRecorder()
		var target targetType
		if err := decodeJSON(rec, req, &target); err == nil {
			t.Fatal("expected error on trailing json")
		}
	})
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
		Education: []education{{
			Institution: "University", Degree: "BSc", StartDate: "2020-01-01",
		}},
		Certifications: []certification{{
			Name: "Cert 1",
		}},
		Skills:    []skill{{Name: "Go", Category: "Backend", Level: 4}},
		TechStack: []tech{{Name: "Go", Category: "Backend"}},
		Projects:  []project{{Title: "Portfolio", Year: 2025}},
	}
	if err := validatePortfolio(valid); err != nil {
		t.Fatalf("valid portfolio rejected: %v", err)
	}

	// Test profile empty fields
	t.Run("missing profile name", func(t *testing.T) {
		p := valid
		p.Profile.Name = ""
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for missing profile name")
		}
	})

	t.Run("missing profile role", func(t *testing.T) {
		p := valid
		p.Profile.Role = ""
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for missing profile role")
		}
	})

	t.Run("missing profile headline", func(t *testing.T) {
		p := valid
		p.Profile.Headline = ""
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for missing profile headline")
		}
	})

	t.Run("missing profile about", func(t *testing.T) {
		p := valid
		p.Profile.About = ""
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for missing profile about")
		}
	})

	t.Run("missing profile location", func(t *testing.T) {
		p := valid
		p.Profile.Location = ""
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for missing profile location")
		}
	})

	// Test experiences
	t.Run("invalid experience start date", func(t *testing.T) {
		p := valid
		p.Experiences = []experience{{Company: "A", Role: "B", StartDate: "yesterday"}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for invalid start date")
		}
	})

	t.Run("experience end date before start date", func(t *testing.T) {
		endDate := "2023-01-01"
		p := valid
		p.Experiences = []experience{{Company: "A", Role: "B", StartDate: "2024-01-01", EndDate: &endDate}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error when end date is before start date")
		}
	})

	t.Run("experience missing company or role", func(t *testing.T) {
		p := valid
		p.Experiences = []experience{{Company: "", Role: "B", StartDate: "2024-01-01"}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for empty company")
		}
		p.Experiences = []experience{{Company: "A", Role: "", StartDate: "2024-01-01"}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for empty role")
		}
	})

	// Test education
	t.Run("education missing institution or degree", func(t *testing.T) {
		p := valid
		p.Education = []education{{Institution: "", Degree: "BSc", StartDate: "2020-01-01"}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for empty institution")
		}
		p.Education = []education{{Institution: "Uni", Degree: "", StartDate: "2020-01-01"}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for empty degree")
		}
	})

	t.Run("education end date before start date", func(t *testing.T) {
		endDate := "2019-01-01"
		p := valid
		p.Education = []education{{Institution: "Uni", Degree: "BSc", StartDate: "2020-01-01", EndDate: &endDate}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error when education end date is before start date")
		}
	})

	// Test certifications
	t.Run("certification missing name or invalid date", func(t *testing.T) {
		p := valid
		p.Certifications = []certification{{Name: ""}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for empty certification name")
		}
		badDate := "invalid-date"
		p.Certifications = []certification{{Name: "Cert", IssuedDate: &badDate}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for invalid certification issued date")
		}
	})

	// Test skills
	t.Run("skill invalid level or missing name/category", func(t *testing.T) {
		p := valid
		p.Skills = []skill{{Name: "", Category: "Backend", Level: 3}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for empty skill name")
		}
		p.Skills = []skill{{Name: "Go", Category: "", Level: 3}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for empty skill category")
		}
		p.Skills = []skill{{Name: "Go", Category: "Backend", Level: 0}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for skill level < 1")
		}
		p.Skills = []skill{{Name: "Go", Category: "Backend", Level: 6}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for skill level > 5")
		}
	})

	// Test techStack
	t.Run("tech stack missing name or category", func(t *testing.T) {
		p := valid
		p.TechStack = []tech{{Name: "", Category: "Backend"}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for empty tech name")
		}
		p.TechStack = []tech{{Name: "Go", Category: ""}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for empty tech category")
		}
	})

	// Test projects
	t.Run("project missing title or invalid year", func(t *testing.T) {
		p := valid
		p.Projects = []project{{Title: "", Year: 2024}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for empty project title")
		}
		p.Projects = []project{{Title: "Project", Year: 1800}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for project year < 1900")
		}
		p.Projects = []project{{Title: "Project", Year: 2300}}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for project year > 2200")
		}
	})

	// Test ui texts
	t.Run("ui texts limits", func(t *testing.T) {
		p := valid
		p.Texts = map[string]string{
			strings.Repeat("k", 65): "value",
		}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for UI text key > 60 chars")
		}
		p.Texts = map[string]string{
			"key": strings.Repeat("v", 405),
		}
		if err := validatePortfolio(p); err == nil {
			t.Fatal("expected error for UI text value > 400 chars")
		}
	})
}

func TestRequireText(t *testing.T) {
	if err := requireText("field", "  "); err == nil {
		t.Fatal("expected error for whitespace string")
	}
	if err := requireText("field", "value"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidDate(t *testing.T) {
	if err := validDate("field", "", false); err != nil {
		t.Fatalf("optional empty date should be valid, got: %v", err)
	}
	if err := validDate("field", "", true); err == nil {
		t.Fatal("required empty date should return error")
	}
	if err := validDate("field", "2024-05-15", true); err != nil {
		t.Fatalf("valid YYYY-MM-DD should pass, got: %v", err)
	}
	if err := validDate("field", "15-05-2024", true); err == nil {
		t.Fatal("invalid date format should fail")
	}
}

func TestValidDatePointer(t *testing.T) {
	if err := validDatePointer("field", nil); err != nil {
		t.Fatalf("nil date pointer should pass, got: %v", err)
	}
	empty := ""
	if err := validDatePointer("field", &empty); err != nil {
		t.Fatalf("empty date pointer should pass, got: %v", err)
	}
	validDateStr := "2025-01-01"
	if err := validDatePointer("field", &validDateStr); err != nil {
		t.Fatalf("valid date pointer should pass, got: %v", err)
	}
	invalidDateStr := "not-a-date"
	if err := validDatePointer("field", &invalidDateStr); err == nil {
		t.Fatal("invalid date pointer should fail")
	}
}

func TestNullableDate(t *testing.T) {
	if val := nullableDate(nil); val != nil {
		t.Errorf("expected nil, got %v", val)
	}
	empty := ""
	if val := nullableDate(&empty); val != nil {
		t.Errorf("expected nil for empty string, got %v", val)
	}
	date := "2025-10-10"
	if val := nullableDate(&date); val != "2025-10-10" {
		t.Errorf("expected '2025-10-10', got %v", val)
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusCreated, map[string]string{"result": "success"})

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Errorf("expected application/json Content-Type, got %q", rec.Header().Get("Content-Type"))
	}
}
