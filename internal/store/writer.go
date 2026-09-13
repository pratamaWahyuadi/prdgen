package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Store struct {
	Dir string
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("store: create dir %q: %w", dir, err)
	}
	return &Store{Dir: dir}, nil
}

func (s *Store) Save(filename, content string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("store: refusing to save empty content to %q", filename)
	}
	path := filepath.Join(s.Dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("store: write %q: %w", path, err)
	}
	return path, nil
}

func (s *Store) Load(filename string) (string, error) {
	path := filepath.Join(s.Dir, filename)
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("store: read %q: %w", path, err)
	}
	return string(b), nil
}

func (s *Store) Exists(filename string) bool {
	path := filepath.Join(s.Dir, filename)
	_, err := os.Stat(path)
	return err == nil
}

func (s *Store) IsComplete(filename string) bool {
	path := filepath.Join(s.Dir, filename)
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() > 0
}

func (s *Store) Append(filename, line string) error {
	path := filepath.Join(s.Dir, filename)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("store: open %q for append: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("store: append to %q: %w", path, err)
	}
	return nil
}

const (
	FileIdea               = "00_idea.md"
	FileDiscoveryQuestions = "01a_discovery_questions.md"
	FileDiscoveryQA        = "01b_discovery_qa.md"
	FileProductBrief       = "01c_product_brief.md"
	FileDeepDiveQuestions  = "01d_deep_dive_questions.md"
	FileDeepDiveQA         = "01e_deep_dive_qa.md"
	FileDefaultsYAML       = "defaults.yaml"
	FileThreatReport       = "02_threat_report.md"
	FilePRD                = "PRD.md"
	FilePRDValidation      = "PRD_VALIDATION.md"
	FileSchema             = "03_schema.md"
	FileAPIContracts       = "04_api_contracts.md"
	FileCodingPlan         = "LLD_PLAN.md"
	FileLLDValidation      = "LLD_VALIDATION.md"
	FileIssuesJSON         = "ISSUES.json"
	FileIssuesCreatedLog   = "ISSUES_CREATED.log"
)
