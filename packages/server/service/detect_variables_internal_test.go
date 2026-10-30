package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// An app's variables are the names its example env file gives, then those its
// code reads in any of the common ways, each once; tests, dependencies and
// what the system sets are left out.
func TestDetectVariables(t *testing.T) {
	got := detectVariables(view(map[string]string{
		".env.example":        "# Apify\nAPIFY_TOKEN=\nexport LOG_LEVEL=info\nlower=no\n",
		"app/main.py":         "import os\ntoken = os.environ['APIFY_TOKEN']\nurl = os.getenv(\"DATABASE_URL\")\nx = os.environ.get('SCRAPE_HOUR', '6')\n",
		"server.js":           "const port = process.env.PORT\nconst s = process.env[\"SESSION_SECRET\"]\nif (process.env.NODE_ENV) {}\n",
		"cmd/main.go":         "v := os.Getenv(\"SMTP_HOST\")\n_, ok := os.LookupEnv(\"FEATURE_X\")\n",
		"lib/a.rb":            "ENV.fetch('RAILS_KEY')\nENV[\"REDIS_URL\"]\n",
		"tests/test_main.py":  "os.environ['ONLY_IN_TESTS']\n",
		"node_modules/x/y.js": "process.env.FROM_A_DEPENDENCY\n",
		"README.md":           "Set process.env.NOT_CODE\n",
		"web/vendor.min.js":   "process.env.MINIFIED\n",
	}))
	names := []string{}
	for _, v := range got {
		names = append(names, v.Name+":"+v.From)
	}
	assert.Equal(t, []string{
		"APIFY_TOKEN:example", "LOG_LEVEL:example",
		"DATABASE_URL:code", "FEATURE_X:code", "PORT:code", "RAILS_KEY:code", "REDIS_URL:code",
		"SCRAPE_HOUR:code", "SESSION_SECRET:code", "SMTP_HOST:code",
	}, names)
}
