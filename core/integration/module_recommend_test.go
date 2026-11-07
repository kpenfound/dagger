package core

import (
	"context"
	"os"
	"path/filepath"

	"github.com/dagger/testctx"
	"github.com/stretchr/testify/require"
)

func (WorkspaceSuite) TestModuleRecommend(ctx context.Context, t *testctx.T) {
	workdir := t.TempDir()
	initGitRepo(ctx, t, workdir)

	out, err := hostDaggerExecRaw(ctx, t, workdir, "mod", "recommend", "--auto-apply")
	require.NoError(t, err, "%s", out)
	require.Contains(t, string(out), "No recommendations.")
	require.NoFileExists(t, filepath.Join(workdir, "dagger.toml"))

	require.NoError(t, os.WriteFile(filepath.Join(workdir, ".ruff.toml"), []byte("line-length = 88\n"), 0o644))
	for _, command := range []string{"mod", "module"} {
		out, err := hostDaggerExecRaw(ctx, t, workdir, command, "recommend")
		require.NoError(t, err, "%s", out)
		require.Contains(t, string(out), "No modules were installed. Run the commands for the modules you select.")
		require.Contains(t, string(out), "dagger module install dagger.io/python/ruff")
		require.NotContains(t, string(out), "Cloud account")
		require.NotContains(t, string(out), "Workspace migration")
		require.NotContains(t, string(out), "Setup complete.")
		require.NoFileExists(t, filepath.Join(workdir, "dagger.toml"))
	}
}

// Exercise the real command, registry predicates and engine workspace from a
// nested cwd. Output must retain root-relative paths at every scope.
func (WorkspaceSuite) TestModuleRecommendScope(ctx context.Context, t *testctx.T) {
	root := t.TempDir()
	initGitRepo(ctx, t, root)
	for name, contents := range map[string]string{
		"go.mod":                             "module example.com/root\n",
		"docs/go.mod":                        "module example.com/docs\n",
		"pyproject.toml":                     "[tool.ruff]\n",
		"examples/celery/pyproject.toml":     "[tool.ruff]\n",
		"app/go.mod":                         "module example.com/app\n",
		"app/pyproject.toml":                 "[tool.ruff]\n",
		"app/examples/celery/go.mod":         "module example.com/celery\n",
		"app/examples/celery/pyproject.toml": "[tool.ruff]\n",
		"sibling/.mocharc.json":              "{}\n",
	} {
		filename := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0o755))
		require.NoError(t, os.WriteFile(filename, []byte(contents), 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "empty"), 0o755))
	cwd := filepath.Join(root, "app")
	for _, tc := range []struct {
		name               string
		args               []string
		goMatch, ruffMatch string
	}{
		{"cwd", nil, "app/go.mod", "app/pyproject.toml"},
		{"whole workspace", []string{"--path", "/"}, "go.mod", "pyproject.toml"},
		{"relative", []string{"--path", "examples"}, "app/examples/celery/go.mod", "app/examples/celery/pyproject.toml"},
		{"absolute", []string{"--path", "/docs"}, "docs/go.mod", ""},
	} {
		args := append([]string{"module", "recommend"}, tc.args...)
		out, err := hostDaggerExecRaw(ctx, t, cwd, args...)
		require.NoError(t, err, "%s: %s", tc.name, out)
		require.Contains(t, string(out), "go: found "+tc.goMatch+"\n")
		if tc.ruffMatch == "" {
			require.NotContains(t, string(out), "ruff: found")
		} else {
			require.Contains(t, string(out), "ruff: found "+tc.ruffMatch+"\n")
		}
		if tc.name != "whole workspace" {
			require.NotContains(t, string(out), "mochajs: found")
		}
		require.Contains(t, string(out), "No modules were installed.")
	}
	for _, selection := range []string{"../..", "missing", "/missing"} {
		out, err := hostDaggerExecRaw(ctx, t, cwd, "module", "recommend", "--path", selection)
		require.Error(t, err, "%s", out)
		require.Contains(t, string(out), selection)
	}
	out, err := hostDaggerExecRaw(ctx, t, cwd, "module", "recommend", "--path", "../empty")
	require.NoError(t, err, "%s", out)
	require.Contains(t, string(out), "No recommendations.")

	// The root and similarly prefixed sibling configs would both fail a
	// content-based scan. Neither may be read when scanning app/.
	require.NoError(t, os.Remove(filepath.Join(root, "pyproject.toml")))
	require.NoError(t, os.Symlink("missing.toml", filepath.Join(root, "pyproject.toml")))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "application"), 0o755))
	require.NoError(t, os.Symlink("missing.toml", filepath.Join(root, "application/pyproject.toml")))
	out, err = hostDaggerExecRaw(ctx, t, cwd, "module", "recommend")
	require.NoError(t, err, "%s", out)
	require.Contains(t, string(out), "ruff: found app/pyproject.toml\n")
	require.NoFileExists(t, filepath.Join(cwd, "dagger.toml"))
}
