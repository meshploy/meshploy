package setup

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The setup page cannot import the console's components: it is one embedded
// file with no build step, served before Docker, the API or the console exist.
// What it shares is the design system by copy - the same token values. This
// test is what keeps the copy honest, so a change to the console's theme is
// noticed here rather than on a gateway six months later.
//
// It is skipped when the console's source is not beside this module, which is
// how the CLI builds on its own.
func TestTheWizardUsesTheConsoleTokens(t *testing.T) {
	const consoleCSS = "../../../web/src/index.css"
	b, err := os.ReadFile(consoleCSS)
	if os.IsNotExist(err) {
		t.Skip("the console's source is not here; nothing to compare against")
	}
	if err != nil {
		t.Fatal(err)
	}

	console := tokensIn(block(string(b), ".dark {"))
	wizard := tokensIn(block(string(wizardHTML), ":root {"))
	if len(wizard) == 0 || len(console) == 0 {
		t.Fatal("could not read the tokens out of either file")
	}

	for name, want := range console {
		got, ok := wizard[name]
		if !ok || strings.HasPrefix(got, "var(") || strings.HasPrefix(want, "var(") {
			// The page defines only the tokens it uses, and a token defined as
			// another token carries no value to compare.
			continue
		}
		if got != want {
			t.Errorf("--%s is %s on the setup page and %s in the console (%s); copy the console's value across",
				name, got, want, consoleCSS)
		}
	}
}

// block returns the declarations of the first rule opening with head.
func block(src, head string) string {
	i := strings.Index(src, head)
	if i < 0 {
		return ""
	}
	rest := src[i+len(head):]
	if j := strings.Index(rest, "}"); j >= 0 {
		return rest[:j]
	}
	return rest
}

var tokenRE = regexp.MustCompile(`--([a-z0-9-]+)\s*:\s*([^;]+);`)

func tokensIn(decls string) map[string]string {
	out := map[string]string{}
	for _, m := range tokenRE.FindAllStringSubmatch(decls, -1) {
		out[m[1]] = strings.Join(strings.Fields(m[2]), " ")
	}
	return out
}
