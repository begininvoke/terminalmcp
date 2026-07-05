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

// autoSkill picks the most relevant skill for a goal and injects its full
// playbook into the agent's context — so the agent always has an expert method
// to follow without having to remember to call search_skills.
func (a *Agent) autoSkill(goal string) string {
	if goal == "" || len(a.skillIndex()) == 0 {
		return ""
	}
	g := strings.ToLower(goal)
	q := g
	switch {
	case strings.Contains(g, "idor"), strings.Contains(g, "bola"):
		q = "idor"
	case strings.Contains(g, "xss"), strings.Contains(g, "cross-site"), strings.Contains(g, "cross site"):
		q = "xss"
	case strings.Contains(g, "sql"):
		q = "sql injection"
	case strings.Contains(g, "ssrf"):
		q = "ssrf"
	case strings.Contains(g, "access"):
		q = "access control"
	case strings.Contains(g, "auth"):
		q = "authentication"
	}
	cands := a.searchSkills(q, 40)
	if len(cands) == 0 {
		if f := strings.Fields(q); len(f) > 0 {
			cands = a.searchSkills(f[0], 40)
		}
	}
	if len(cands) == 0 {
		return ""
	}
	best := bestSkill(cands, strings.Fields(q))
	body, err := a.loadSkillBody(best)
	if err != nil {
		return ""
	}
	if len(body) > 3500 {
		body = body[:3500] + "\n...[truncated — call load_skill(\"" + best + "\") for the full playbook]"
	}
	return "\n\n=== EXPERT PLAYBOOK: " + best + " (FOLLOW THIS to reach the goal; search_skills for more) ===\n" + body
}

// bestSkill ranks candidates: keyword-in-name and offensive verbs score high,
// defensive/analysis skills score low — so we pick an exploitation playbook.
func bestSkill(cands []SkillMeta, qwords []string) string {
	offensive := []string{"exploiting-", "testing-", "performing-", "attacking-", "abusing-", "bypassing-", "finding-", "identifying-", "fuzzing-"}
	defensive := []string{"analyzing-", "detecting-", "auditing-", "monitoring-", "investigating-", "responding-", "hardening-", "reviewing-", "hunting-", "triaging-"}
	best, bestScore := cands[0].Name, -1<<30
	for _, s := range cands {
		name := strings.ToLower(s.Name)
		score := 0
		for _, w := range qwords {
			if strings.Contains(name, w) {
				score += 10
			}
		}
		for _, p := range offensive {
			if strings.HasPrefix(name, p) {
				score += 6
				break
			}
		}
		for _, p := range defensive {
			if strings.HasPrefix(name, p) {
				score -= 6
				break
			}
		}
		if score > bestScore {
			bestScore, best = score, s.Name
		}
	}
	return best
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
