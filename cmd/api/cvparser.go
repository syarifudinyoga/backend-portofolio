package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dslipak/pdf"
)

type parsedCVResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message,omitempty"`
	Found   parsedCVData    `json:"found"`
	Summary parsedCVSummary `json:"summary"`
}

type parsedCVData struct {
	Profile        map[string]string `json:"profile"`
	Experiences    []experience      `json:"experiences"`
	Education      []education       `json:"education"`
	Certifications []certification   `json:"certifications"`
	Skills         []skill           `json:"skills"`
	TechStack      []tech            `json:"techStack"`
	Projects       []project         `json:"projects"`
}

type parsedCVSummary struct {
	FieldsFound         []string `json:"fieldsFound"`
	ExperiencesCount    int      `json:"experiencesCount"`
	EducationCount      int      `json:"educationCount"`
	CertificationsCount int      `json:"certificationsCount"`
	SkillsCount         int      `json:"skillsCount"`
	TechStackCount      int      `json:"techStackCount"`
	ProjectsCount       int      `json:"projectsCount"`
}

var monthNames = map[string]int{
	"january": 1, "jan": 1, "januari": 1,
	"february": 2, "feb": 2, "februari": 2,
	"march": 3, "mar": 3, "maret": 3,
	"april": 4, "apr": 4,
	"may": 5, "mei": 5,
	"june": 6, "jun": 6, "juni": 6,
	"july": 7, "jul": 7, "juli": 7,
	"august": 8, "aug": 8, "agustus": 8,
	"september": 9, "sep": 9, "sept": 9,
	"october": 10, "oct": 10, "oktober": 10, "okt": 10,
	"november": 11, "nov": 11,
	"december": 12, "dec": 12, "desember": 12, "des": 12,
}

func handleParseCV(w http.ResponseWriter, r *http.Request, logger *log.Logger) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(25 << 20); err != nil {
		http.Error(w, "file terlalu besar atau format multipart tidak valid", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file CV wajib diunggah (form field: 'file')", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "gagal membaca file yang diunggah", http.StatusInternalServerError)
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	var rawText string

	switch ext {
	case ".pdf":
		text, err := extractPdfText(fileBytes)
		if err != nil {
			logger.Printf("parse pdf error: %v", err)
			http.Error(w, fmt.Sprintf("gagal mengekstrak teks dari PDF: %v", err), http.StatusBadRequest)
			return
		}
		rawText = text
	case ".docx":
		text, err := extractDocxText(fileBytes)
		if err != nil {
			logger.Printf("parse docx error: %v", err)
			http.Error(w, fmt.Sprintf("gagal mengekstrak teks dari DOCX: %v", err), http.StatusBadRequest)
			return
		}
		rawText = text
	case ".json":
		var imported portfolio
		if err := json.Unmarshal(fileBytes, &imported); err == nil && imported.Profile.Name != "" {
			writeImportedPortfolioJSON(w, imported)
			return
		}
		rawText = string(fileBytes)
	default:
		rawText = string(fileBytes)
	}

	preprocessed := preprocessCVText(rawText)
	parsed := parseCVText(preprocessed)

	fieldsFound := make([]string, 0)
	for k, v := range parsed.Profile {
		if strings.TrimSpace(v) != "" {
			fieldsFound = append(fieldsFound, k)
		}
	}

	summary := parsedCVSummary{
		FieldsFound:         fieldsFound,
		ExperiencesCount:    len(parsed.Experiences),
		EducationCount:      len(parsed.Education),
		CertificationsCount: len(parsed.Certifications),
		SkillsCount:         len(parsed.Skills),
		TechStackCount:      len(parsed.TechStack),
		ProjectsCount:       len(parsed.Projects),
	}

	resp := parsedCVResponse{
		Success: true,
		Found:   parsed,
		Summary: summary,
	}

	if err := writeEncryptedJSON(w, http.StatusOK, resp); err != nil {
		logger.Printf("write encrypted parse-cv response: %v", err)
	}
}

func writeImportedPortfolioJSON(w http.ResponseWriter, p portfolio) {
	fieldsFound := []string{}
	if p.Profile.Name != "" {
		fieldsFound = append(fieldsFound, "name")
	}
	if p.Profile.Role != "" {
		fieldsFound = append(fieldsFound, "role")
	}
	if p.Profile.Email != "" {
		fieldsFound = append(fieldsFound, "email")
	}
	if p.Profile.Location != "" {
		fieldsFound = append(fieldsFound, "location")
	}
	if p.Profile.Headline != "" {
		fieldsFound = append(fieldsFound, "headline")
	}
	if p.Profile.About != "" {
		fieldsFound = append(fieldsFound, "about")
	}
	if p.Profile.GithubURL != "" {
		fieldsFound = append(fieldsFound, "githubUrl")
	}
	if p.Profile.LinkedinURL != "" {
		fieldsFound = append(fieldsFound, "linkedinUrl")
	}

	profMap := map[string]string{
		"name":         p.Profile.Name,
		"role":         p.Profile.Role,
		"headline":     p.Profile.Headline,
		"about":        p.Profile.About,
		"location":     p.Profile.Location,
		"email":        p.Profile.Email,
		"website":      p.Profile.Website,
		"avatarUrl":    p.Profile.AvatarURL,
		"resumeUrl":    p.Profile.ResumeURL,
		"githubUrl":    p.Profile.GithubURL,
		"instagramUrl": p.Profile.InstagramURL,
		"twitterUrl":   p.Profile.TwitterURL,
		"linkedinUrl":  p.Profile.LinkedinURL,
	}

	resp := parsedCVResponse{
		Success: true,
		Found: parsedCVData{
			Profile:        profMap,
			Experiences:    p.Experiences,
			Education:      p.Education,
			Certifications: p.Certifications,
			Skills:         p.Skills,
			TechStack:      p.TechStack,
			Projects:       p.Projects,
		},
		Summary: parsedCVSummary{
			FieldsFound:         fieldsFound,
			ExperiencesCount:    len(p.Experiences),
			EducationCount:      len(p.Education),
			CertificationsCount: len(p.Certifications),
			SkillsCount:         len(p.Skills),
			TechStackCount:      len(p.TechStack),
			ProjectsCount:       len(p.Projects),
		},
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(resp)
}

func extractPdfText(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	b, err := r.GetPlainText()
	if err != nil {
		return "", err
	}
	if _, err := buf.ReadFrom(b); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func extractDocxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			defer rc.Close()
			xmlBytes, err := io.ReadAll(rc)
			if err != nil {
				return "", err
			}
			s := string(xmlBytes)
			s = regexp.MustCompile(`</w:p>`).ReplaceAllString(s, "\n")
			s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
			return s, nil
		}
	}
	return "", fmt.Errorf("word/document.xml tidak ditemukan di dalam arsip docx")
}

func preprocessCVText(raw string) string {
	res := raw
	res = strings.ReplaceAll(res, "\r\n", "\n")
	res = strings.ReplaceAll(res, "\r", "\n")

	// Standard ligatures
	ligatures := map[string]string{
		"\uFB00": "ff",
		"\uFB01": "fi",
		"\uFB02": "fl",
		"\uFB03": "ffi",
		"\uFB04": "ffl",
		"\uFB05": "ft",
		"\uFB06": "st",
	}
	for lig, repl := range ligatures {
		res = strings.ReplaceAll(res, lig, repl)
	}

	// Fix common words broken by replacement char
	res = regexp.MustCompile(`(?i)so\x{FFFD}ware`).ReplaceAllString(res, "Software")
	res = regexp.MustCompile(`(?i)applica\x{FFFD}on`).ReplaceAllString(res, "application")
	res = regexp.MustCompile(`(?i)interac\x{FFFD}ve`).ReplaceAllString(res, "interactive")
	res = regexp.MustCompile(`(?i)visualiza\x{FFFD}on`).ReplaceAllString(res, "visualization")
	res = regexp.MustCompile(`(?i)na\x{FFFD}ve`).ReplaceAllString(res, "native")
	res = regexp.MustCompile(`(?i)na\x{FFFD}onal`).ReplaceAllString(res, "national")
	res = regexp.MustCompile(`(?i)solu\x{FFFD}on`).ReplaceAllString(res, "solution")
	res = regexp.MustCompile(`(?i)automa\x{FFFD}on`).ReplaceAllString(res, "automation")
	res = regexp.MustCompile(`(?i)automa\x{FFFD}ng`).ReplaceAllString(res, "automating")
	res = regexp.MustCompile(`(?i)elas\x{FFFD}csearch`).ReplaceAllString(res, "Elasticsearch")
	res = regexp.MustCompile(`(?i)suppor\x{FFFD}ng`).ReplaceAllString(res, "supporting")
	res = regexp.MustCompile(`(?i)ac\x{FFFD}ve`).ReplaceAllString(res, "active")
	res = regexp.MustCompile(`(?i)tra\x{FFFD}c`).ReplaceAllString(res, "traffic")
	res = regexp.MustCompile(`(?i)real-\x{FFFD}me`).ReplaceAllString(res, "real-time")
	res = regexp.MustCompile(`(?i)synchroniza\x{FFFD}on`).ReplaceAllString(res, "synchronization")
	res = regexp.MustCompile(`(?i)op\x{FFFD}mized`).ReplaceAllString(res, "optimized")
	res = regexp.MustCompile(`(?i)cri\x{FFFD}cal`).ReplaceAllString(res, "critical")
	res = regexp.MustCompile(`(?i)twi\x{FFFD}er`).ReplaceAllString(res, "Twitter")
	res = regexp.MustCompile(`(?i)work\x{FFFD}ow`).ReplaceAllString(res, "workflow")
	res = regexp.MustCompile(`(?i)transac\x{FFFD}onal`).ReplaceAllString(res, "transactional")
	res = regexp.MustCompile(`(?i)repe\x{FFFD}ve`).ReplaceAllString(res, "repetitive")
	res = regexp.MustCompile(`(?i)e\x{FFFD}ciency`).ReplaceAllString(res, "efficiency")
	res = regexp.MustCompile(`(?i)opera\x{FFFD}onal`).ReplaceAllString(res, "operational")
	res = regexp.MustCompile(`(?i)informa\x{FFFD}cs`).ReplaceAllString(res, "Informatics")
	res = regexp.MustCompile(`(?i)concentra\x{FFFD}on`).ReplaceAllString(res, "Concentration")
	res = regexp.MustCompile(`(?i)cer\x{FFFD}ficate`).ReplaceAllString(res, "Certificate")
	res = regexp.MustCompile(`(?i)pro\x{FFFD}ciency`).ReplaceAllString(res, "Proficiency")
	res = regexp.MustCompile(`(?i)pla\x{FFFD}orm`).ReplaceAllString(res, "Platform")
	res = regexp.MustCompile(`(?i)mul\x{FFFD}-a\x{FFFD}ribute`).ReplaceAllString(res, "Multi-Attribute")
	res = regexp.MustCompile(`(?i)u\x{FFFD}lity`).ReplaceAllString(res, "Utility")
	res = regexp.MustCompile(`(?i)recommenda\x{FFFD}on`).ReplaceAllString(res, "Recommendation")

	res = regexp.MustCompile(`([a-zA-Z])\x{FFFD}([a-zA-Z])`).ReplaceAllString(res, "${1}ti${2}")
	res = strings.ReplaceAll(res, "\uFFFD", "")

	// Insert newlines and markers before primary section headings in a single robust pass
	reSections := regexp.MustCompile(`(?:\s{2,}|\n|^)(PROFESSIONAL SUMMARY|SUMMARY|PROFIL PROFESIONAL|RINGKASAN|TENTANG SAYA|TECHNICAL SKILLS|SKILLS & TECHNOLOGIES|SKILLS|KEAHLIAN TEKNIS|KEAHLIAN|KETERAMPILAN|PROFESSIONAL EXPERIENCE|WORK EXPERIENCE|EXPERIENCE|PENGALAMAN KERJA|PENGALAMAN PROFESIONAL|PENGALAMAN|EDUCATION & QUALIFICATIONS|EDUCATION|PENDIDIKAN|RIWAYAT PENDIDIKAN|LICENSES & CERTIFICATIONS|CERTIFICATIONS|CERTIFICATES|SERTIFIKASI|SERTIFIKAT|KEY PROJECTS|SELECTED PROJECTS|PROJECTS|PORTFOLIO|PROYEK PILIHAN|PROYEK|LANGUAGES|BAHASA)(?:\s{1,}|\n|$)`)
	res = reSections.ReplaceAllStringFunc(res, func(m string) string {
		upper := strings.ToUpper(strings.TrimSpace(m))
		key := "UNKNOWN"
		if strings.Contains(upper, "SUMMARY") || strings.Contains(upper, "PROFIL") || strings.Contains(upper, "RINGKASAN") || strings.Contains(upper, "TENTANG") {
			key = "SUMMARY"
		} else if strings.Contains(upper, "SKILL") || strings.Contains(upper, "KEAHLIAN") || strings.Contains(upper, "KETERAMPILAN") {
			key = "SKILLS"
		} else if strings.Contains(upper, "EXPERIENCE") || strings.Contains(upper, "PENGALAMAN") {
			key = "EXPERIENCE"
		} else if strings.Contains(upper, "EDUCATION") || strings.Contains(upper, "PENDIDIKAN") {
			key = "EDUCATION"
		} else if strings.Contains(upper, "CERTIFICAT") || strings.Contains(upper, "SERTIFIKAS") || strings.Contains(upper, "SERTIFIKAT") {
			key = "CERTIFICATIONS"
		} else if strings.Contains(upper, "PROJECT") || strings.Contains(upper, "PORTFOLIO") || strings.Contains(upper, "PROYEK") {
			key = "PROJECTS"
		} else if strings.Contains(upper, "LANGUAGE") || strings.Contains(upper, "BAHASA") {
			key = "LANGUAGES"
		}
		return "\n\n===HEADER:" + key + "===\n"
	})

	// Insert newlines before bullets
	res = regexp.MustCompile(`\s*•\s*`).ReplaceAllString(res, "\n• ")
	res = regexp.MustCompile(`\s+o\s+`).ReplaceAllString(res, "\n  o ")

	// In SKILLS section, insert newlines before category headers like "Languages & Frameworks:" or "Cloud & DevOps:"
	res = regexp.MustCompile(`\s+([A-Za-z\s&]{3,30}:)`).ReplaceAllString(res, "\n$1")

	res = strings.ReplaceAll(res, "–", "-")
	res = strings.ReplaceAll(res, "—", "-")
	return res
}

func parseCVText(text string) parsedCVData {
	data := parsedCVData{
		Profile:        make(map[string]string),
		Experiences:    make([]experience, 0),
		Education:      make([]education, 0),
		Certifications: make([]certification, 0),
		Skills:         make([]skill, 0),
		TechStack:      make([]tech, 0),
		Projects:       make([]project, 0),
	}

	// Extract Global Contacts & Links
	emailRe := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	githubRe := regexp.MustCompile(`(?:https?:\/\/)?(?:www\.)?github\.com\/([a-zA-Z0-9_-]+)`)
	linkedinRe := regexp.MustCompile(`(?:https?:\/\/)?(?:www\.)?linkedin\.com\/(?:in\/)?([a-zA-Z0-9_-]+)`)
	instagramRe := regexp.MustCompile(`(?:https?:\/\/)?(?:www\.)?instagram\.com\/([a-zA-Z0-9_.-]+)`)
	twitterRe := regexp.MustCompile(`(?:https?:\/\/)?(?:www\.)?(?:twitter\.com|x\.com)\/([a-zA-Z0-9_]+)`)

	if email := emailRe.FindString(text); email != "" {
		data.Profile["email"] = strings.TrimSpace(email)
	}
	if m := githubRe.FindStringSubmatch(text); len(m) > 1 {
		data.Profile["githubUrl"] = "https://github.com/" + m[1]
	}
	if m := linkedinRe.FindStringSubmatch(text); len(m) > 1 {
		data.Profile["linkedinUrl"] = "https://www.linkedin.com/in/" + m[1]
	}
	if m := instagramRe.FindStringSubmatch(text); len(m) > 1 {
		data.Profile["instagramUrl"] = "https://www.instagram.com/" + m[1]
	}
	if m := twitterRe.FindStringSubmatch(text); len(m) > 1 {
		data.Profile["twitterUrl"] = "https://x.com/" + m[1]
	}

	sections := splitCVSections(text)

	// Profile Header
	extractProfileHeader(sections["HEADER"], data.Profile)

	// Summary
	summaryText := sections["SUMMARY"]
	if summaryText == "" {
		summaryText = sections["PROFESSIONAL SUMMARY"]
	}
	if summaryText != "" {
		cleanSummary := strings.TrimSpace(summaryText)
		data.Profile["about"] = cleanSummary
		if data.Profile["headline"] == "" {
			firstSentence := strings.SplitN(cleanSummary, ".", 2)[0]
			if firstSentence != "" {
				data.Profile["headline"] = strings.TrimSpace(firstSentence) + "."
			}
		}
		if data.Profile["role"] == "" {
			roleRe := regexp.MustCompile(`(?i)(Fullstack\s+Software\s+Engineer|Fullstack\s+Developer|Backend\s+Developer|Frontend\s+Developer|Software\s+Engineer|(?:Fullstack|Backend|Frontend|Software|Data|DevOps|Cloud|Mobile|Web)\s+(?:Developer|Engineer|Architect|Specialist))`)
			if m := roleRe.FindString(cleanSummary); m != "" {
				data.Profile["role"] = strings.TrimSpace(m)
			}
		}
	}

	// Skills
	skillsText := sections["TECHNICAL SKILLS"]
	if skillsText == "" {
		skillsText = sections["SKILLS"]
	}
	if skillsText != "" {
		extractSkills(skillsText, &data.Skills, &data.TechStack)
	}

	// Experience
	expText := sections["PROFESSIONAL EXPERIENCE"]
	if expText == "" {
		expText = sections["WORK EXPERIENCE"]
	}
	if expText == "" {
		expText = sections["EXPERIENCE"]
	}
	if expText != "" {
		data.Experiences = extractExperiences(expText)
	}

	// Education
	eduText := sections["EDUCATION"]
	if eduText != "" {
		data.Education = extractEducation(eduText)
	}

	// Certifications
	certText := sections["CERTIFICATIONS"]
	if certText != "" {
		data.Certifications = extractCertifications(certText)
	}

	// Projects
	projText := sections["PROJECTS"]
	if projText == "" {
		projText = sections["KEY PROJECTS"]
	}
	if projText != "" {
		data.Projects = extractProjects(projText)
	}

	return data
}

func splitCVSections(text string) map[string]string {
	result := make(map[string]string)
	markerRe := regexp.MustCompile(`===HEADER:([^=]+)===`)

	locs := markerRe.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		result["HEADER"] = text
		return result
	}

	// Text before first marker is HEADER
	result["HEADER"] = strings.TrimSpace(text[:locs[0][0]])

	for i := 0; i < len(locs); i++ {
		headerName := strings.ToUpper(strings.TrimSpace(text[locs[i][2]:locs[i][3]]))
		bodyStart := locs[i][1]
		bodyEnd := len(text)
		if i+1 < len(locs) {
			bodyEnd = locs[i+1][0]
		}
		body := strings.TrimSpace(text[bodyStart:bodyEnd])
		result[headerName] = body
	}

	return result
}

func extractProfileHeader(headerText string, prof map[string]string) {
	lines := strings.Split(headerText, "\n")
	var candidates []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			candidates = append(candidates, trimmed)
		}
	}

	if len(candidates) == 0 {
		return
	}

	firstLine := candidates[0]
	pipeIdx := strings.Index(firstLine, "|")
	if pipeIdx > 0 {
		prof["name"] = strings.TrimSpace(firstLine[:pipeIdx])
	} else {
		emailRe := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
		if m := emailRe.FindStringIndex(firstLine); len(m) > 0 {
			prof["name"] = strings.TrimSpace(firstLine[:m[0]])
		} else {
			prof["name"] = strings.TrimSpace(firstLine)
		}
	}

	// Strip address/phone from name if attached
	prof["name"] = regexp.MustCompile(`(?i)(Cimalaka|Jakarta|Bandung|Surabaya|Indonesia|\d{5}|08\d{2}|\+62).*`).ReplaceAllString(prof["name"], "")
	prof["name"] = strings.Trim(prof["name"], " |,-")

	// Location
	locRe := regexp.MustCompile(`(?i)([A-Za-z\s]+(?:,\s*[A-Za-z\s]+)*\s+\d{5}|(?:Jakarta|Bandung|Sumedang|Surabaya|Yogyakarta|Semarang|Bali|Medan|Indonesia)[^|\n]*)`)
	if m := locRe.FindString(headerText); m != "" {
		cleanedLoc := strings.TrimSpace(m)
		cleanedLoc = regexp.MustCompile(`\+?\d[\d\s-]{8,}`).ReplaceAllString(cleanedLoc, "")
		cleanedLoc = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+`).ReplaceAllString(cleanedLoc, "")
		cleanedLoc = strings.Trim(cleanedLoc, " |,-")
		if cleanedLoc != "" {
			prof["location"] = strings.TrimSpace(cleanedLoc)
			if prof["name"] != "" {
				prof["location"] = strings.TrimSpace(strings.ReplaceAll(prof["location"], prof["name"], ""))
				prof["location"] = strings.Trim(prof["location"], " |,-")
			}
		}
	}

	// Role
	for i := 1; i < len(candidates) && i < 4; i++ {
		line := candidates[i]
		if strings.Contains(line, "@") || strings.Contains(line, "http") || strings.Contains(line, "+62") {
			continue
		}
		if regexp.MustCompile(`(?i)(developer|engineer|architect|designer|manager|lead)`).MatchString(line) {
			prof["role"] = strings.TrimSpace(line)
			break
		}
	}
}

func extractSkills(text string, skills *[]skill, techStack *[]tech) {
	lines := strings.Split(text, "\n")
	seenTech := make(map[string]bool)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		parts := strings.SplitN(trimmed, ":", 2)
		category := "Teknologi"
		itemsStr := trimmed

		if len(parts) == 2 && len(parts[0]) < 40 {
			category = strings.TrimSpace(parts[0])
			category = strings.TrimPrefix(category, "• ")
			category = strings.TrimPrefix(category, "- ")
			itemsStr = parts[1]
		}

		items := strings.Split(itemsStr, ",")
		for _, rawItem := range items {
			name := strings.TrimSpace(rawItem)
			name = strings.TrimPrefix(name, "• ")
			name = strings.TrimPrefix(name, "- ")
			name = strings.TrimPrefix(name, "and ")
			name = strings.TrimSpace(name)

			if name == "" || len(name) > 50 || strings.HasPrefix(strings.ToLower(name), "such as") {
				continue
			}

			if !seenTech[strings.ToLower(name)] {
				seenTech[strings.ToLower(name)] = true
				*skills = append(*skills, skill{
					Name:     name,
					Category: category,
					Level:    4,
				})
				*techStack = append(*techStack, tech{
					Name:     name,
					Category: category,
				})
			}
		}
	}
}

func extractExperiences(text string) []experience {
	var list []experience
	blocks := splitJobBlocks(text)

	for _, block := range blocks {
		exp := parseSingleExperience(block)
		if exp.Company != "" {
			list = append(list, exp)
		}
	}

	return list
}

func splitJobBlocks(text string) []string {
	var blocks []string
	lines := strings.Split(text, "\n")
	var current []string

	dateLineRe := regexp.MustCompile(`(?i)\b(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec|Januari|Februari|Maret|April|Mei|Juni|Juli|Agustus|September|Oktober|November|Desember|\d{4})\b.*\b(Present|Sekarang|Current|Saat Ini|\d{4})\b`)

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}

		isNewJob := false
		if strings.HasPrefix(trimmed, "•") && (strings.Contains(trimmed, "|") || dateLineRe.MatchString(trimmed)) {
			isNewJob = true
		} else if strings.Contains(trimmed, " | ") && dateLineRe.MatchString(trimmed) && len(current) > 1 {
			isNewJob = true
		}

		if isNewJob && len(current) > 0 {
			blocks = append(blocks, strings.Join(current, "\n"))
			current = []string{trimmed}
		} else {
			current = append(current, trimmed)
		}
	}

	if len(current) > 0 {
		blocks = append(blocks, strings.Join(current, "\n"))
	}

	return blocks
}

func parseSingleExperience(block string) experience {
	exp := experience{
		Highlights: make([]string, 0),
	}

	lines := strings.Split(block, "\n")
	if len(lines) == 0 {
		return exp
	}

	firstLine := strings.TrimSpace(lines[0])
	firstLine = strings.TrimPrefix(firstLine, "• ")
	firstLine = strings.TrimPrefix(firstLine, "- ")

	dateRe := regexp.MustCompile(`(?i)\b([A-Za-z]+|\d{1,2})\s+(\d{4})\s*[-–—]\s*(Present|Sekarang|Current|Saat Ini|[A-Za-z]+\s+\d{4}|\d{1,2}\s+\d{4}|\d{4})\b`)
	if m := dateRe.FindString(block); m != "" {
		startDate, endDate := parseDateRange(m)
		exp.StartDate = startDate
		exp.EndDate = endDate
	}

	firstLineClean := dateRe.ReplaceAllString(firstLine, "")
	parts := strings.Split(firstLineClean, "|")
	if len(parts) >= 2 {
		exp.Company = strings.TrimSpace(parts[0])
		exp.Role = strings.TrimSpace(parts[1])
	} else {
		dashParts := strings.Split(firstLineClean, " - ")
		if len(dashParts) >= 2 {
			exp.Company = strings.TrimSpace(dashParts[0])
			exp.Role = strings.TrimSpace(dashParts[1])
		} else {
			exp.Company = strings.TrimSpace(firstLineClean)
			if len(lines) > 1 && !strings.HasPrefix(lines[1], "o") && !strings.HasPrefix(lines[1], "•") {
				exp.Role = strings.TrimSpace(lines[1])
			}
		}
	}

	for i := 1; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		if dateRe.MatchString(l) && len(l) < 35 {
			continue
		}
		bulletClean := strings.TrimPrefix(l, "o ")
		bulletClean = strings.TrimPrefix(bulletClean, "• ")
		bulletClean = strings.TrimPrefix(bulletClean, "- ")
		bulletClean = strings.TrimSpace(bulletClean)
		if bulletClean != "" && bulletClean != exp.Role {
			exp.Highlights = append(exp.Highlights, bulletClean)
		}
	}

	if len(exp.Highlights) > 0 {
		exp.Description = exp.Highlights[0]
	}

	if exp.StartDate == "" {
		exp.StartDate = fmt.Sprintf("%d-01-01", time.Now().Year())
	}

	return exp
}

func extractEducation(text string) []education {
	var list []education
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return list
	}

	firstLine := strings.TrimSpace(lines[0])
	firstLine = strings.TrimPrefix(firstLine, "• ")
	firstLine = strings.TrimPrefix(firstLine, "- ")

	edu := education{}
	dateRe := regexp.MustCompile(`(?i)\b([A-Za-z]+|\d{1,2})\s+(\d{4})\s*[-–—]\s*(Present|Sekarang|Current|[A-Za-z]+\s+\d{4}|\d{4})\b`)
	if m := dateRe.FindString(text); m != "" {
		startDate, endDate := parseDateRange(m)
		edu.StartDate = startDate
		edu.EndDate = endDate
	}

	firstLineClean := dateRe.ReplaceAllString(firstLine, "")
	parts := strings.Split(firstLineClean, "|")
	if len(parts) >= 2 {
		edu.Institution = strings.TrimSpace(parts[0])
		edu.Degree = strings.TrimSpace(parts[1])
	} else {
		edu.Institution = strings.TrimSpace(firstLineClean)
		edu.Degree = "Bachelor"
	}

	var descParts []string
	for i := 1; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		if dateRe.MatchString(l) && len(l) < 35 {
			continue
		}
		cleanLine := strings.TrimPrefix(l, "• ")
		cleanLine = strings.TrimPrefix(cleanLine, "o ")
		cleanLine = strings.TrimPrefix(cleanLine, "- ")
		cleanLine = strings.TrimSpace(cleanLine)

		if strings.HasPrefix(strings.ToLower(cleanLine), "major:") || strings.HasPrefix(strings.ToLower(cleanLine), "jurusan:") {
			edu.Field = strings.TrimSpace(strings.SplitN(cleanLine, ":", 2)[1])
		} else if strings.HasPrefix(strings.ToLower(cleanLine), "concentration:") || strings.HasPrefix(strings.ToLower(cleanLine), "konsentrasi:") {
			edu.Field = strings.TrimSpace(strings.SplitN(cleanLine, ":", 2)[1])
			descParts = append(descParts, cleanLine)
		} else {
			descParts = append(descParts, cleanLine)
		}
	}

	if edu.Field == "" && strings.Contains(strings.ToLower(edu.Degree), "engineering") {
		edu.Field = "Informatics Engineering"
	}

	edu.Description = strings.Join(descParts, " · ")
	if edu.StartDate == "" {
		edu.StartDate = "2016-09-01"
	}

	if edu.Institution != "" {
		list = append(list, edu)
	}

	return list
}

func extractCertifications(text string) []certification {
	var list []certification
	lines := strings.Split(text, "\n")

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		clean := strings.TrimPrefix(trimmed, "• ")
		clean = strings.TrimPrefix(clean, "- ")
		clean = strings.TrimSpace(clean)

		parts := strings.Split(clean, " - ")
		if len(parts) < 2 {
			parts = strings.Split(clean, "–")
		}

		cert := certification{
			Name: clean,
		}

		if len(parts) >= 2 {
			cert.Name = strings.TrimSpace(parts[0])
			cert.Issuer = strings.TrimSpace(parts[1])
		} else if strings.Contains(clean, "(") && strings.Contains(clean, ")") {
			openParen := strings.Index(clean, "(")
			closeParen := strings.LastIndex(clean, ")")
			cert.Name = strings.TrimSpace(clean[:openParen])
			cert.Issuer = strings.TrimSpace(clean[openParen+1 : closeParen])
		}

		if cert.Name != "" {
			list = append(list, cert)
		}
	}

	return list
}

func extractProjects(text string) []project {
	var list []project
	lines := strings.Split(text, "\n")
	var current *project

	yearRe := regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}

		isNew := strings.HasPrefix(trimmed, "• ") || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "# ")
		clean := strings.TrimPrefix(trimmed, "• ")
		clean = strings.TrimPrefix(clean, "- ")
		clean = strings.TrimPrefix(clean, "# ")
		clean = strings.TrimSpace(clean)

		if isNew || current == nil {
			if current != nil && current.Title != "" {
				list = append(list, *current)
			}
			yr := time.Now().Year()
			if m := yearRe.FindString(clean); m != "" {
				fmt.Sscanf(m, "%d", &yr)
			}
			current = &project{
				Title:       clean,
				ProjectType: "personal",
				Year:        yr,
				Tech:        make([]string, 0),
			}
		} else {
			if strings.HasPrefix(strings.ToLower(clean), "tech:") || strings.HasPrefix(strings.ToLower(clean), "stack:") {
				techStr := strings.TrimSpace(strings.SplitN(clean, ":", 2)[1])
				for _, t := range strings.Split(techStr, ",") {
					if tt := strings.TrimSpace(t); tt != "" {
						current.Tech = append(current.Tech, tt)
					}
				}
			} else {
				if current.Description == "" {
					current.Description = clean
				} else {
					current.Description += " " + clean
				}
			}
		}
	}

	if current != nil && current.Title != "" {
		list = append(list, *current)
	}

	return list
}

func parseDateRange(dateStr string) (string, *string) {
	parts := regexp.MustCompile(`[-–—]`).Split(dateStr, 2)
	if len(parts) == 0 {
		return "", nil
	}

	start := parseSingleDate(parts[0], true)
	if len(parts) < 2 {
		return start, nil
	}

	endRaw := strings.TrimSpace(parts[1])
	if regexp.MustCompile(`(?i)\b(present|sekarang|current|saat ini)\b`).MatchString(endRaw) {
		return start, nil
	}

	end := parseSingleDate(endRaw, false)
	if end != "" {
		return start, &end
	}
	return start, nil
}

func parseSingleDate(raw string, isStart bool) string {
	cleaned := strings.ToLower(strings.TrimSpace(raw))
	yearRe := regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
	year := yearRe.FindString(cleaned)
	if year == "" {
		return ""
	}

	month := 1
	if !isStart {
		month = 12
	}

	for name, num := range monthNames {
		if strings.Contains(cleaned, name) {
			month = num
			break
		}
	}

	day := 1
	if !isStart {
		switch month {
		case 2:
			day = 28
		case 4, 6, 9, 11:
			day = 30
		default:
			day = 31
		}
	}

	return fmt.Sprintf("%s-%02d-%02d", year, month, day)
}
