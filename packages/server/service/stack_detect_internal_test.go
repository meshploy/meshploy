package service

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func view(files map[string]string) repoView {
	sizes := map[string]int64{}
	for p, c := range files {
		sizes[p] = int64(len(c))
	}
	return repoView{sizes: sizes, read: func(p string) string { return files[p] }}
}

// docai-backend as it was: a FastAPI app in app/main.py loading torch, with a
// Dockerfile that starts it on 8080. Its Dockerfile is the builder to use.
func TestDetectPrefersADockerfileThatStartsTheApp(t *testing.T) {
	d := suggest(detectStack(view(map[string]string{
		"requirements.txt": "fastapi\nuvicorn\nsentence_transformers==2.2\ntorch\n",
		"app/main.py":      "from fastapi import FastAPI\napp: FastAPI = FastAPI(title=\"x\")\n",
		"Dockerfile":       "FROM python:3.12\nEXPOSE 8080\nCMD [\"uvicorn\", \"app.main:app\"]\n",
		"tests/test_x.py":  "app = FastAPI()\n",
	}), ""))
	assert.Equal(t, "Python · FastAPI (app/main.py) · Dockerfile", d.Summary)
	assert.Equal(t, "dockerfile", d.Builder)
	assert.Equal(t, 8080, d.Port)
	assert.Empty(t, d.StartCommand)
	assert.Equal(t, "2Gi", d.MemoryLimit)
	assert.Equal(t, []string{"sentence-transformers", "torch"}, d.Facts.Heavy)
}

// Without a Dockerfile, Railpack, with a start command for the app it found.
func TestDetectSuggestsAStartCommandForRailpack(t *testing.T) {
	d := suggest(detectStack(view(map[string]string{
		"pyproject.toml": "[project]\ndependencies = [\"flask\"]\n",
		"src/web.py":     "from flask import Flask\nserver = Flask(__name__)\n",
	}), ""))
	assert.Equal(t, "railpack", d.Builder)
	assert.Equal(t, "gunicorn src.web:server --bind 0.0.0.0:$PORT", d.StartCommand)
	assert.Equal(t, 5000, d.Port)
	assert.Empty(t, d.MemoryLimit)

	dj := suggest(detectStack(view(map[string]string{"requirements.txt": "Django", "manage.py": "", "mysite/wsgi.py": ""}), ""))
	assert.Equal(t, "gunicorn mysite.wsgi --bind 0.0.0.0:$PORT", dj.StartCommand)

	unknown := suggest(detectStack(view(map[string]string{"requirements.txt": "requests"}), ""))
	assert.Empty(t, unknown.StartCommand)
	assert.Contains(t, unknown.Notes[0], "No app entry point found")
}

// A Node app with a start script needs nothing; one without gets its entry file.
func TestDetectNode(t *testing.T) {
	withStart := suggest(detectStack(view(map[string]string{"package.json": `{"scripts":{"start":"node x.js"},"dependencies":{"puppeteer":"1"}}`}), ""))
	assert.Empty(t, withStart.StartCommand)
	assert.Equal(t, "1Gi", withStart.MemoryLimit)
	assert.Equal(t, "Node.js", withStart.Summary)

	bare := suggest(detectStack(view(map[string]string{"package.json": `{}`, "server.js": ""}), ""))
	assert.Equal(t, "node server.js", bare.StartCommand)
	assert.Equal(t, 3000, bare.Port)
}

// The clone reads the tree and small files only: a large file is listed, but
// never fetched or read.
func TestShallowRepoViewReadsSmallFilesOnly(t *testing.T) {
	src := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", src}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	git("init", "-q", "-b", "main")
	git("config", "uploadpack.allowFilter", "true")
	require.NoError(t, os.MkdirAll(filepath.Join(src, "app"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "app/main.py"), []byte("app = FastAPI()\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "requirements.txt"), []byte("fastapi\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "model.bin"), bytes.Repeat([]byte("x"), maxDetectRead+10), 0o644))
	git("add", ".")
	git("commit", "-q", "-m", "init")

	r, cleanup, err := shallowRepoView(context.Background(), gitCredentials{URL: "file://" + src}, "main")
	require.NoError(t, err)
	defer cleanup()
	assert.True(t, r.has("model.bin"))
	assert.Empty(t, r.read("model.bin"))
	assert.Equal(t, "fastapi\n", r.read("requirements.txt"))
	assert.Equal(t, "app.main:app", detectStack(r, "").Entry)

	_, _, err = shallowRepoView(context.Background(), gitCredentials{URL: "file://" + src}, "nope")
	assert.Error(t, err)
}
