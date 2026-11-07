package daggercmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"dagger.io/dagger"
	"dagger.io/dagger/core"
	"github.com/stretchr/testify/require"
)

func TestModuleRecommendCommand(t *testing.T) {
	root := testRootCommand()
	for _, name := range []string{"module", "mod"} {
		cmd, args, err := root.Find([]string{name, "recommend"})
		require.NoError(t, err)
		require.Same(t, moduleRecommendCmd, cmd)
		require.Empty(t, args)
		require.NoError(t, cmd.Args(cmd, nil))
		require.Error(t, cmd.Args(cmd, []string{"extra"}))
		require.NoError(t, validateFlagCapabilities(root, []string{name, "recommend", "-W", "/tmp/workspace", "-y"}))
	}
	require.True(t, commandShowsFinalProgress(moduleRecommendCmd))
	require.Equal(t, workspaceFlagPolicyLocalOnly, workspaceFlagPolicy(moduleRecommendCmd, nil))
	help := renderHelp(t, moduleRecommendCmd)
	require.Contains(t, help, "--auto-apply")
	require.Contains(t, help, "--workspace")
	require.Contains(t, help, "current working directory's subtree")
	require.Contains(t, help, "--path")
	require.Contains(t, help, "--path /")
}

func TestSelectRecommendedModulesAutoApply(t *testing.T) {
	previous := autoApply
	autoApply = true
	t.Cleanup(func() { autoApply = previous })
	recs := []recommendation{
		{Module: registryModule{Name: "ruff", Repo: "dagger.io/python/ruff"}, Match: "ruff.toml"},
		{Module: registryModule{Name: "go", Repo: "dagger.io/go"}, Match: "go.mod"},
	}
	selected, err := selectRecommendedModules(t.Context(), recs, nil)
	require.NoError(t, err)
	require.Equal(t, recs, selected)
}

func TestSelectRecommendedModulesNonInteractive(t *testing.T) {
	previousAutoApply, previousStdin := autoApply, os.Stdin
	stdin, writer, err := os.Pipe()
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	autoApply, os.Stdin = false, stdin
	t.Cleanup(func() {
		autoApply, os.Stdin = previousAutoApply, previousStdin
		require.NoError(t, stdin.Close())
	})
	selected, err := selectRecommendedModules(t.Context(), []recommendation{
		{Module: registryModule{Name: "ruff", Repo: "dagger.io/python/ruff"}, Match: "ruff.toml"},
	}, nil)
	require.NoError(t, err)
	require.Empty(t, selected)
}

// A scope belongs only to discovery; installing its selection still writes
// the workspace root configuration, using the caller's original workspace.
func TestScopedRecommendationInstallWorkspaceWide(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	git := exec.CommandContext(ctx, "git", "init", root)
	out, err := git.CombinedOutput()
	require.NoError(t, err, "%s", out)
	for name, contents := range map[string]string{
		"app/config/marker": "",
		"dep/dagger.json":   `{"name":"dep","sdk":{"source":"dang"}}`,
		"dep/main.dang":     "type Dep { pub hello: String! { \"hello\" } }\n",
	} {
		filename := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0o755))
		require.NoError(t, os.WriteFile(filename, []byte(contents), 0o644))
	}
	dag, err := dagger.Connect(ctx, dagger.WithWorkdir(filepath.Join(root, "app")))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, dag.Close()) })
	recs, err := recommendModules(ctx, core.NewQuery(dag).CurrentWorkspace(), "config", []registryModule{{
		Name: "dep", Repo: "../dep", Recommend: SimpleRecommend("**/marker"),
	}}, nil)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	require.Equal(t, "app/config/marker", recs[0].Match)
	require.NoError(t, installRecommended(ctx, dag, recs, nil))
	config, err := os.ReadFile(filepath.Join(root, "dagger.toml"))
	require.NoError(t, err)
	require.Contains(t, string(config), "[modules.dep]")
	require.Contains(t, string(config), `source = "dep"`)
	require.NoFileExists(t, filepath.Join(root, "app/dagger.toml"))
}
