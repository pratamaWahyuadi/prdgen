package prompts

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed discovery.txt discovery_brief.txt discovery_deep.txt defaults_gen.txt security.txt prd.txt prd_consistency.txt lld_erd.txt lld_api.txt lld_plan.txt lld_consistency.txt gh_issues.txt gh_issue_revise.txt document_revise.txt
var embedded embed.FS

type Name string

const (
	Discovery      Name = "discovery"
	DiscoveryBrief Name = "discovery_brief"
	DiscoveryDeep  Name = "discovery_deep"
	DefaultsGen    Name = "defaults_gen"
	Security       Name = "security"
	PRD            Name = "prd"
	PRDConsistency Name = "prd_consistency"
	LLDErd         Name = "lld_erd"
	LLDApi         Name = "lld_api"
	LLDPlan        Name = "lld_plan"
	LLDConsistency Name = "lld_consistency"
	GHIssues       Name = "gh_issues"
	GHIssueRevise  Name = "gh_issue_revise"
	DocumentRevise Name = "document_revise"
)

var filenames = map[Name]string{
	Discovery:      "discovery.txt",
	DiscoveryBrief: "discovery_brief.txt",
	DiscoveryDeep:  "discovery_deep.txt",
	DefaultsGen:    "defaults_gen.txt",
	Security:       "security.txt",
	PRD:            "prd.txt",
	PRDConsistency: "prd_consistency.txt",
	LLDErd:         "lld_erd.txt",
	LLDApi:         "lld_api.txt",
	LLDPlan:        "lld_plan.txt",
	LLDConsistency: "lld_consistency.txt",
	GHIssues:       "gh_issues.txt",
	GHIssueRevise:  "gh_issue_revise.txt",
	DocumentRevise: "document_revise.txt",
}

func Load(n Name) (string, error) {
	fname, ok := filenames[n]
	if !ok {
		return "", fmt.Errorf("prompts: unknown prompt name %q", n)
	}
	b, err := embedded.ReadFile(fname)
	if err != nil {
		return "", fmt.Errorf("prompts: read embedded %q: %w", fname, err)
	}
	return string(b), nil
}

func LoadFromDir(dir string, n Name) (string, error) {
	fname, ok := filenames[n]
	if !ok {
		return "", fmt.Errorf("prompts: unknown prompt name %q", n)
	}
	path := filepath.Join(dir, fname)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Load(n)
		}
		return "", fmt.Errorf("prompts: read %q: %w", path, err)
	}
	return string(b), nil
}
