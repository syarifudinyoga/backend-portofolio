package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPreprocessCVText(t *testing.T) {
	raw := "so\uFFFDware engineer\r\n" +
		"applica\uFFFDon\r\n" +
		"• bullet item\n" +
		"o sub bullet\n" +
		"SUMMARY\n" +
		"Experienced developer\n" +
		"SKILLS\n" +
		"Languages: Go, Python\n" +
		"EXPERIENCE\n" +
		"Company A – 2021 – 2023\n"

	processed := preprocessCVText(raw)

	if strings.Contains(processed, "\r") {
		t.Error("carriage returns should have been normalized")
	}
	if !strings.Contains(processed, "Software") {
		t.Errorf("expected 'Software', got %q", processed)
	}
	if !strings.Contains(processed, "application") {
		t.Errorf("expected 'application', got %q", processed)
	}
	if !strings.Contains(processed, "===HEADER:SUMMARY===") {
		t.Errorf("expected section marker for SUMMARY, got %q", processed)
	}
	if !strings.Contains(processed, "===HEADER:SKILLS===") {
		t.Errorf("expected section marker for SKILLS, got %q", processed)
	}
	if !strings.Contains(processed, "===HEADER:EXPERIENCE===") {
		t.Errorf("expected section marker for EXPERIENCE, got %q", processed)
	}
}

func TestSplitCVSections(t *testing.T) {
	text := "John Doe | Software Engineer\n" +
		"===HEADER:SUMMARY===\n" +
		"Passionate backend engineer with 5 years experience.\n" +
		"===HEADER:SKILLS===\n" +
		"Go, PostgreSQL, Docker\n"

	sections := splitCVSections(text)
	if !strings.Contains(sections["HEADER"], "John Doe") {
		t.Errorf("expected John Doe in HEADER, got %q", sections["HEADER"])
	}
	if !strings.Contains(sections["SUMMARY"], "Passionate backend engineer") {
		t.Errorf("expected summary text, got %q", sections["SUMMARY"])
	}
	if !strings.Contains(sections["SKILLS"], "Go, PostgreSQL") {
		t.Errorf("expected skills text, got %q", sections["SKILLS"])
	}
}

func TestExtractProfileHeader(t *testing.T) {
	headerText := "Jane Doe\n" +
		"Senior Backend Engineer\n" +
		"jane.doe@example.com | Jakarta, Indonesia | +6281234567890\n"

	prof := make(map[string]string)
	extractProfileHeader(headerText, prof)

	if prof["name"] != "Jane Doe" {
		t.Errorf("expected name 'Jane Doe', got %q", prof["name"])
	}
	if prof["role"] != "Senior Backend Engineer" {
		t.Errorf("expected role 'Senior Backend Engineer', got %q", prof["role"])
	}
	if !strings.Contains(prof["location"], "Jakarta") {
		t.Errorf("expected location containing 'Jakarta', got %q", prof["location"])
	}
}

func TestExtractSkills(t *testing.T) {
	skillsText := "Languages & Frameworks: Go, Python, React, Vue\n" +
		"Databases: PostgreSQL, Redis, PostgreSQL\n"

	var skills []skill
	var techStack []tech

	extractSkills(skillsText, &skills, &techStack)

	if len(skills) != 6 { // Go, Python, React, Vue, PostgreSQL, Redis (deduped)
		t.Errorf("expected 6 deduplicated skills, got %d", len(skills))
	}
	if len(techStack) != 6 {
		t.Errorf("expected 6 deduplicated tech items, got %d", len(techStack))
	}

	foundGo := false
	for _, s := range skills {
		if s.Name == "Go" && s.Category == "Languages & Frameworks" && s.Level == 4 {
			foundGo = true
			break
		}
	}
	if !foundGo {
		t.Error("expected Go with category 'Languages & Frameworks' and level 4")
	}
}

func TestExtractExperiences(t *testing.T) {
	expText := "• Tech Corp | Senior Go Developer\n" +
		"January 2021 – Present\n" +
		"• Built high-throughput microservices in Go\n" +
		"• Integrated Kafka for event streaming\n\n" +
		"• Startup Inc | Software Engineer\n" +
		"June 2019 – December 2020\n" +
		"• Maintained legacy monolithic system\n"

	experiences := extractExperiences(expText)
	if len(experiences) != 2 {
		t.Fatalf("expected 2 experiences, got %d", len(experiences))
	}

	first := experiences[0]
	if first.Company != "Tech Corp" {
		t.Errorf("expected company 'Tech Corp', got %q", first.Company)
	}
	if first.Role != "Senior Go Developer" {
		t.Errorf("expected role 'Senior Go Developer', got %q", first.Role)
	}
	if first.StartDate != "2021-01-01" {
		t.Errorf("expected start date '2021-01-01', got %q", first.StartDate)
	}
	if first.EndDate != nil {
		t.Errorf("expected nil EndDate for present, got %v", first.EndDate)
	}
	if len(first.Highlights) < 2 {
		t.Errorf("expected at least 2 highlights, got %d", len(first.Highlights))
	}

	second := experiences[1]
	if second.Company != "Startup Inc" {
		t.Errorf("expected company 'Startup Inc', got %q", second.Company)
	}
	if second.StartDate != "2019-06-01" {
		t.Errorf("expected start date '2019-06-01', got %q", second.StartDate)
	}
	if second.EndDate == nil || *second.EndDate != "2020-12-31" {
		t.Errorf("expected end date '2020-12-31', got %v", second.EndDate)
	}
}

func TestExtractEducation(t *testing.T) {
	eduText := "• Bandung Institute of Technology | Bachelor of Engineering\n" +
		"August 2016 – September 2020\n" +
		"• Major: Informatics Engineering\n"

	educations := extractEducation(eduText)
	if len(educations) != 1 {
		t.Fatalf("expected 1 education item, got %d", len(educations))
	}

	edu := educations[0]
	if edu.Institution != "Bandung Institute of Technology" {
		t.Errorf("expected institution 'Bandung Institute of Technology', got %q", edu.Institution)
	}
	if edu.Degree != "Bachelor of Engineering" {
		t.Errorf("expected degree 'Bachelor of Engineering', got %q", edu.Degree)
	}
	if edu.Field != "Informatics Engineering" {
		t.Errorf("expected field 'Informatics Engineering', got %q", edu.Field)
	}
	if edu.StartDate != "2016-08-01" {
		t.Errorf("expected start date '2016-08-01', got %q", edu.StartDate)
	}
	if edu.EndDate == nil || *edu.EndDate != "2020-09-30" {
		t.Errorf("expected end date '2020-09-30', got %v", edu.EndDate)
	}
}

func TestExtractCertifications(t *testing.T) {
	certText := "• Certified Kubernetes Administrator - Linux Foundation\n" +
		"• AWS Certified Solutions Architect (Amazon Web Services)\n"

	certs := extractCertifications(certText)
	if len(certs) != 2 {
		t.Fatalf("expected 2 certifications, got %d", len(certs))
	}

	if certs[0].Name != "Certified Kubernetes Administrator" || certs[0].Issuer != "Linux Foundation" {
		t.Errorf("unexpected cert 0: %+v", certs[0])
	}
	if certs[1].Name != "AWS Certified Solutions Architect" || certs[1].Issuer != "Amazon Web Services" {
		t.Errorf("unexpected cert 1: %+v", certs[1])
	}
}

func TestExtractProjects(t *testing.T) {
	projText := "• Cloud Monitor 2024\n" +
		"Tech: Go, Prometheus, Grafana\n" +
		"Real-time monitoring system for distributed microservices.\n" +
		"• AI Bot 2023\n" +
		"Stack: Python, FastAPI\n" +
		"An intelligent assistant.\n"

	projects := extractProjects(projText)
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}

	p1 := projects[0]
	if p1.Title != "Cloud Monitor 2024" {
		t.Errorf("expected title 'Cloud Monitor 2024', got %q", p1.Title)
	}
	if p1.Year != 2024 {
		t.Errorf("expected year 2024, got %d", p1.Year)
	}
	if len(p1.Tech) != 3 {
		t.Errorf("expected 3 tech stack items, got %d", len(p1.Tech))
	}
	if !strings.Contains(p1.Description, "Real-time monitoring system") {
		t.Errorf("unexpected description: %q", p1.Description)
	}
}

func TestParseDateRangeAndSingleDate(t *testing.T) {
	t.Run("Indonesian months and present", func(t *testing.T) {
		start, end := parseDateRange("Januari 2022 - Sekarang")
		if start != "2022-01-01" {
			t.Errorf("expected start '2022-01-01', got %q", start)
		}
		if end != nil {
			t.Errorf("expected nil end date for 'Sekarang', got %v", end)
		}
	})

	t.Run("Specific month range in English", func(t *testing.T) {
		start, end := parseDateRange("March 2020 - November 2021")
		if start != "2020-03-01" {
			t.Errorf("expected start '2020-03-01', got %q", start)
		}
		if end == nil || *end != "2021-11-30" {
			t.Errorf("expected end '2021-11-30', got %v", end)
		}
	})

	t.Run("February leap/end calculation", func(t *testing.T) {
		end := parseSingleDate("Februari 2023", false)
		if end != "2023-02-28" {
			t.Errorf("expected '2023-02-28', got %q", end)
		}
	})

	t.Run("Empty or invalid date", func(t *testing.T) {
		d := parseSingleDate("no-date-here", true)
		if d != "" {
			t.Errorf("expected empty string, got %q", d)
		}
		start, end := parseDateRange("")
		if start != "" || end != nil {
			t.Errorf("expected empty date range, got %q, %v", start, end)
		}
	})
}

func TestExtractDocxText(t *testing.T) {
	t.Run("valid docx archive", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)

		f, err := zw.Create("word/document.xml")
		if err != nil {
			t.Fatalf("create document.xml failed: %v", err)
		}
		xmlContent := `<w:document><w:body><w:p><w:r><w:t>Hello from DOCX!</w:t></w:r></w:p></w:body></w:document>`
		if _, err := f.Write([]byte(xmlContent)); err != nil {
			t.Fatalf("write failed: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("close zip failed: %v", err)
		}

		text, err := extractDocxText(buf.Bytes())
		if err != nil {
			t.Fatalf("extractDocxText failed: %v", err)
		}
		if !strings.Contains(text, "Hello from DOCX!") {
			t.Fatalf("expected extracted text to contain 'Hello from DOCX!', got %q", text)
		}
	})

	t.Run("invalid docx without document.xml", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		f, _ := zw.Create("other.xml")
		_, _ = f.Write([]byte("<other/>"))
		_ = zw.Close()

		_, err := extractDocxText(buf.Bytes())
		if err == nil {
			t.Fatal("expected error when word/document.xml is missing")
		}
	})

	t.Run("corrupted zip", func(t *testing.T) {
		_, err := extractDocxText([]byte("not a zip file"))
		if err == nil {
			t.Fatal("expected error for corrupted zip")
		}
	})
}

func TestHandleParseCV(t *testing.T) {
	logger := log.New(io.Discard, "", 0)

	t.Run("Method Not Allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/parse-cv", nil)
		rec := httptest.NewRecorder()
		handleParseCV(rec, req, logger)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 Method Not Allowed, got %d", rec.Code)
		}
	})

	t.Run("Missing File Field", func(t *testing.T) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/admin/parse-cv", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()

		handleParseCV(rec, req, logger)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("JSON portfolio import", func(t *testing.T) {
		p := portfolio{
			Profile: profile{
				Name:        "Test Developer",
				Role:        "Go Engineer",
				Headline:    "Building scalable tools",
				About:       "Experienced in backend engineering",
				Location:    "Jakarta, ID",
				Email:       "test@example.com",
				GithubURL:   "https://github.com/test",
				LinkedinURL: "https://linkedin.com/in/test",
			},
		}
		raw, _ := json.Marshal(p)

		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("file", "portfolio.json")
		_, _ = part.Write(raw)
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/admin/parse-cv", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()

		handleParseCV(rec, req, logger)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp parsedCVResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.Found.Profile["name"] != "Test Developer" {
			t.Errorf("expected name 'Test Developer', got %q", resp.Found.Profile["name"])
		}
	})

	t.Run("Plain text CV file", func(t *testing.T) {
		cvContent := "John Doe | Senior Developer\njohn@example.com\nSUMMARY\nGreat developer.\nSKILLS\nGo, Docker\n"
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("file", "cv.txt")
		_, _ = part.Write([]byte(cvContent))
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/admin/parse-cv", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()

		handleParseCV(rec, req, logger)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		// Response is encrypted with writeEncryptedJSON
		var env encryptedEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("failed to unmarshal encrypted response: %v", err)
		}
		decrypted, err := decryptData(env.IV, env.Data)
		if err != nil {
			t.Fatalf("failed to decrypt response: %v", err)
		}

		var resp parsedCVResponse
		if err := json.Unmarshal(decrypted, &resp); err != nil {
			t.Fatalf("failed to unmarshal decrypted response: %v", err)
		}
		if !resp.Success {
			t.Fatal("expected success to be true")
		}
	})

	t.Run("Invalid PDF upload returns 400", func(t *testing.T) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("file", "resume.pdf")
		_, _ = part.Write([]byte("not-a-valid-pdf"))
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/admin/parse-cv", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()

		handleParseCV(rec, req, logger)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("DOCX upload parses successfully", func(t *testing.T) {
		var docxBuf bytes.Buffer
		zw := zip.NewWriter(&docxBuf)
		f, err := zw.Create("word/document.xml")
		if err != nil {
			t.Fatalf("create document.xml: %v", err)
		}
		_, _ = f.Write([]byte("<w:document><w:body><w:p><w:r><w:t>Dev User\nSenior Engineer\nSUMMARY\nGreat dev</w:t></w:r></w:p></w:body></w:document>"))
		_ = zw.Close()

		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("file", "resume.docx")
		_, _ = part.Write(docxBuf.Bytes())
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/admin/parse-cv", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()

		handleParseCV(rec, req, logger)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for valid DOCX, got %d", rec.Code)
		}
	})
}

func TestExtractPdfText_Invalid(t *testing.T) {
	_, err := extractPdfText([]byte("random bytes not pdf"))
	if err == nil {
		t.Fatal("expected error extracting text from invalid PDF bytes")
	}
}
