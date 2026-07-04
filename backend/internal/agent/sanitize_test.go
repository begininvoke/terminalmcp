package agent

import (
	"strings"
	"testing"

	"terminalmcp/internal/config"
)

func TestSanitizeCommand(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agent.WordlistsDir = "../../../wordlists"
	if !fileExists(cfg.Agent.WordlistsDir + "/xss.txt") {
		t.Skip("wordlists dir not found from test cwd")
	}
	a := &Agent{cfg: cfg}

	// The exact broken command from the field: unquoted '&' + a bogus wordlist path.
	in := `ffuf -u https://selleracademy.digikala.com/wp-admin/admin-ajax.php?action=some_action&param=FUZZ -w /path/to/xss_wordlist.txt -t 50`
	out, note := a.sanitizeCommand(in)
	t.Logf("note: %s", note)
	t.Logf("fixed: %s", out)

	if !strings.Contains(out, "'https://selleracademy.digikala.com/wp-admin/admin-ajax.php?action=some_action&param=FUZZ'") {
		t.Errorf("URL was not quoted: %s", out)
	}
	if strings.Contains(out, "/path/to/xss_wordlist.txt") {
		t.Errorf("missing wordlist was not replaced: %s", out)
	}
	if !strings.Contains(out, "xss.txt") {
		t.Errorf("expected xss.txt substitution: %s", out)
	}

	// A correct command must be left untouched.
	ok := `curl -s 'https://api.example.com/v1/users/1'`
	if out2, note2 := a.sanitizeCommand(ok); note2 != "" || out2 != ok {
		t.Errorf("well-formed command was modified: %q note=%q", out2, note2)
	}
}
