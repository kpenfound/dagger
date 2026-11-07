package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/creack/pty"
	"github.com/dagger/testctx"
	"github.com/stretchr/testify/require"
)

// The setup flow asks its last questions as plain text after the TUI has
// closed. The TUI's terminal keeps reading stdin for the life of the process,
// so those prompts must read through it; reading os.Stdin directly never
// returns. Only a real pty exercises this.
func (WorkspaceSuite) TestSetupPromptReadsInputAfterTUI(ctx context.Context, t *testctx.T) {
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", "https://github.com/example/project.git"},
	} {
		git := exec.Command("git", args...)
		git.Dir = repo
		out, err := git.CombinedOutput()
		require.NoError(t, err, string(out))
	}

	console, err := newTUIConsole(t, 60*time.Second)
	require.NoError(t, err)
	defer console.Close()

	tty := console.Tty()
	require.NoError(t, pty.Setsize(tty, &pty.Winsize{Rows: 40, Cols: 120}))

	cmd := hostDaggerCommand(ctx, t, repo, "--progress=tty", "init")
	// No Cloud credentials, so enabling checks stops at the signup prompt.
	cmd.Env = append(cmd.Env, "HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir(), "DAGGER_CLOUD_TOKEN=")
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty
	require.NoError(t, cmd.Start())

	// Forms inside the TUI: Run is preselected, so Enter takes it and the
	// right arrow moves to Skip.
	_, err = console.ExpectString("Find and install suitable modules?")
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	_, err = console.Send("\x1b[C\r") // Skip the module recommendations.
	require.NoError(t, err)

	_, err = console.ExpectString("Enable cloud checks?")
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	_, err = console.Send("\r") // Run, reaching the account prerequisite.
	require.NoError(t, err)

	// The TUI is gone now; this prompt runs as a standalone form that must
	// read through the terminal the TUI left behind.
	_, err = console.ExpectString("Sign up or log in?")
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	_, err = console.Send("\x1b[C\r") // Skip, declining the prerequisite.
	require.NoError(t, err)

	_, err = console.ExpectString("Complete the prerequisite")
	require.NoError(t, err, "the prompt never read the answer")

	go console.ExpectEOF()
	require.Error(t, cmd.Wait(), "declining the prerequisite exits non-zero")
}

// Init deliberately opts into a whole-workspace scan, even when started below
// the root. Go and Ruff exercise both filename and content-based markers.
func (WorkspaceSuite) TestInitRecommendationsFromNestedCwd(ctx context.Context, t *testctx.T) {
	root := t.TempDir()
	initGitRepo(ctx, t, root)
	for name, contents := range map[string]string{
		"go.mod":                             "module example.com/root\n",
		"pyproject.toml":                     "[tool.ruff]\n",
		"app/go.mod":                         "module example.com/app\n",
		"app/pyproject.toml":                 "[tool.ruff]\n",
		"app/examples/celery/go.mod":         "module example.com/celery\n",
		"app/examples/celery/pyproject.toml": "[tool.ruff]\n",
	} {
		filename := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0o755))
		require.NoError(t, os.WriteFile(filename, []byte(contents), 0o644))
	}
	console, err := newTUIConsole(t, 60*time.Second)
	require.NoError(t, err)
	defer console.Close()
	tty := console.Tty()
	require.NoError(t, pty.Setsize(tty, &pty.Winsize{Rows: 45, Cols: 140}))
	cmd := hostDaggerCommand(ctx, t, filepath.Join(root, "app"), "--progress=tty", "init")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	require.NoError(t, cmd.Start())
	_, err = console.ExpectString("Find and install suitable modules?")
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	_, err = console.Send("\r")
	require.NoError(t, err)
	_, err = console.ExpectString("go: found go.mod")
	require.NoError(t, err)
	_, err = console.ExpectString("ruff: found pyproject.toml")
	require.NoError(t, err)
	_, err = console.ExpectString("Install selected")
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	_, err = console.Send("\x1b[C\r") // Skip installation.
	require.NoError(t, err)
	_, err = console.ExpectString("Enable cloud checks?")
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	_, err = console.Send("\x1b[C\r")
	require.NoError(t, err)
	go console.ExpectEOF()
	require.NoError(t, cmd.Wait())
	require.FileExists(t, filepath.Join(root, "dagger.toml"))
	require.NoFileExists(t, filepath.Join(root, "app/dagger.toml"))
}
