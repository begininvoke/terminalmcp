package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SkillMeta is one entry from the skills index.json.
type SkillMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

var (
	skillsOnce  sync.Once
	skillsCache []SkillMeta
)

// skillIndex loads and caches the skill catalog from <skills_dir>/index.json.
func (a *Agent) skillIndex() []SkillMeta {
	skillsOnce.Do(func() {
		dir := a.cfg.Agent.SkillsDir
		if dir == "" {
			return
		}
		data, err := os.ReadFile(filepath.Join(dir, "index.json"))
		if err != nil {
			return
		}
		var idx struct {
			Skills []SkillMeta `json:"skills"`
		}
		if json.Unmarshal(data, &idx) == nil {
			skillsCache = idx.Skills
		}
	})
	return skillsCache
}

// searchSkills returns skills whose name/description match all query words.
func (a *Agent) searchSkills(query string, limit int) []SkillMeta {
	if limit <= 0 {
		limit = 12
	}
	words := strings.Fields(strings.ToLower(query))
	var out []SkillMeta
	for _, s := range a.skillIndex() {
		hay := strings.ToLower(s.Name + " " + s.Description)
		match := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				match = false
				break
			}
		}
		if match {
			out = append(out, s)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// loadSkillBody returns the SKILL.md contents for a named skill.
func (a *Agent) loadSkillBody(name string) (string, error) {
	dir := a.cfg.Agent.SkillsDir
	name = strings.TrimSpace(name)
	// Resolve the path via the index; fall back to the conventional layout.
	path := filepath.Join(dir, "skills", name, "SKILL.md")
	for _, s := range a.skillIndex() {
		if s.Name == name {
			path = filepath.Join(dir, s.Path, "SKILL.md")
			break
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// skillHint tells the agent that expert playbooks exist and how to use them.
func (a *Agent) skillHint() string {
	n := len(a.skillIndex())
	if n == 0 {
		return ""
	}
	return "\n\nEXPERT SKILL PLAYBOOKS: " + itoa(n) + " step-by-step cybersecurity skills are available. " +
		"When you start a phase or feel stuck, call search_skills(\"<keywords>\") (e.g. \"idor\", \"xss\", \"jwt\", \"ssrf\", " +
		"\"sql injection\", \"wordpress\", \"privilege escalation\") to find a relevant expert playbook, then load_skill(\"<name>\") " +
		"to read its exact method and FOLLOW it. Prefer a matching skill over improvising."
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
