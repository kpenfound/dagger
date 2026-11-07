package daggercmd

import (
	"context"
	"testing"

	"dagger.io/dagger"
	"dagger.io/dagger/core"
	"github.com/stretchr/testify/require"
)

func TestSimpleRecommend(t *testing.T) {
	ctx := t.Context()
	dag, err := dagger.Connect(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, dag.Close()) })

	dir := core.NewQuery(dag).Directory().
		WithNewFile("go.mod", "module example.com/root").
		WithNewFile("app/go.mod", "module example.com/app").
		WithNewFile("app/go.sum", "")
	for _, excluded := range []string{".git", ".dagger", "node_modules", "vendor", "dist", "build", "target"} {
		dir = dir.
			WithNewFile(excluded+"/go.mod", "module example.com/excluded").
			WithNewFile("app/"+excluded+"/go.mod", "module example.com/excluded")
	}
	ws := dir.AsWorkspace(core.DirectoryAsWorkspaceOpts{Cwd: "/app"})

	matches, err := SimpleRecommend("**/go.mod", "**/go.sum")(ctx, ws)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"app/go.mod", "app/go.sum"}, matches)

	matches, err = SimpleRecommend("**/go.mod")(ctx, ws.WithWorkdir("."))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"go.mod", "app/go.mod"}, matches)

	matches, err = SimpleRecommend("**/missing.toml")(ctx, ws)
	require.NoError(t, err)
	require.Empty(t, matches)

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = SimpleRecommend("**/go.mod")(canceled, ws)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRecommendModules(t *testing.T) {
	ctx := t.Context()
	dag, err := dagger.Connect(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, dag.Close()) })
	ws := core.NewQuery(dag).Directory().WithNewDirectory("app").AsWorkspace(core.DirectoryAsWorkspaceOpts{Cwd: "/app"})

	mods := []registryModule{
		{Name: "z", Recommend: func(ctx context.Context, ws *core.Workspace) ([]string, error) {
			cwd, err := ws.Cwd(ctx)
			require.NoError(t, err)
			require.Equal(t, "/app", cwd)
			return []string{"app/z/config", "app/a/config", "app/a/config", "app/a/deeper/config"}, nil
		}},
		{Name: "installed", Recommend: func(context.Context, *core.Workspace) ([]string, error) {
			t.Fatal("installed modules must not be scanned")
			return nil, nil
		}},
		{Name: "disabled"},
		{Name: "empty", Recommend: func(context.Context, *core.Workspace) ([]string, error) {
			return nil, nil
		}},
		{Name: "a", Recommend: func(context.Context, *core.Workspace) ([]string, error) {
			return []string{"app/other/config"}, nil
		}},
	}
	recs, err := recommendModules(ctx, ws, "", mods, map[string]bool{"installed": true})
	require.NoError(t, err)
	require.Len(t, recs, 2)
	require.Equal(t, "a", recs[0].Module.Name)
	require.Equal(t, "app/other/config", recs[0].Match)
	require.Equal(t, "z", recs[1].Module.Name)
	require.Equal(t, "app/a/config", recs[1].Match)

	_, err = recommendModules(ctx, ws, "", []registryModule{{
		Name: "canceled",
		Recommend: func(context.Context, *core.Workspace) ([]string, error) {
			return nil, context.Canceled
		},
	}}, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestResolveRecommendationPath(t *testing.T) {
	for _, tc := range []struct {
		cwd, selection, want string
	}{
		{"/", "", "."},
		{"/app", "", "app"},
		{"/app", ".", "app"},
		{"/app", "examples/../child", "app/child"},
		{"/app", "../sibling", "sibling"},
		{"/app", "/", "."},
		{"/app", "/sibling", "sibling"},
		{"/app", "/app/../sibling/", "sibling"},
		{"/app", "..", "."},
	} {
		t.Run(tc.cwd+":"+tc.selection, func(t *testing.T) {
			got, err := resolveRecommendationPath(tc.cwd, tc.selection)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
	for _, selection := range []string{"../..", "/../sibling", "/app/../../sibling"} {
		_, err := resolveRecommendationPath("/", selection)
		require.ErrorContains(t, err, selection)
		require.ErrorContains(t, err, "outside the workspace")
	}
}

func TestRecommendModulesScopeAndDepth(t *testing.T) {
	ctx := t.Context()
	dag, err := dagger.Connect(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, dag.Close()) })
	ws := core.NewQuery(dag).Directory().
		WithNewFile("go.mod", "module example.com/root").
		WithNewFile("docs/go.mod", "module example.com/docs").
		WithNewFile("pyproject.toml", "[tool.ruff]").
		WithNewFile("examples/celery/pyproject.toml", "[tool.ruff]").
		WithNewFile("app/pyproject.toml", "[tool.ruff]").
		WithNewFile("app/examples/celery/pyproject.toml", "[tool.ruff]").
		WithNewFile("app/z/marker", "").
		WithNewFile("app/a/marker", "").
		WithNewFile("app/a/deeper/marker", "").
		WithNewDirectory("empty").
		AsWorkspace(core.DirectoryAsWorkspaceOpts{Cwd: "/app"})
	mods := []registryModule{
		{Name: "go", Recommend: SimpleRecommend("**/go.mod")},
		{Name: "python", Recommend: SimpleRecommend("**/pyproject.toml")},
		{Name: "nested", Recommend: SimpleRecommend("**/marker")},
	}
	for _, tc := range []struct {
		scope string
		want  map[string]string
	}{
		{"/", map[string]string{"go": "go.mod", "python": "pyproject.toml", "nested": "app/a/marker"}},
		{"", map[string]string{"python": "app/pyproject.toml", "nested": "app/a/marker"}},
		{"examples", map[string]string{"python": "app/examples/celery/pyproject.toml"}},
		{"/docs", map[string]string{"go": "docs/go.mod"}},
		{"../empty", map[string]string{}},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			recs, err := recommendModules(ctx, ws, tc.scope, mods, nil)
			require.NoError(t, err)
			got := map[string]string{}
			for _, rec := range recs {
				got[rec.Module.Name] = rec.Match
			}
			require.Equal(t, tc.want, got)
		})
	}
	for _, selection := range []string{"../..", "missing", "/missing"} {
		_, err := recommendModules(ctx, ws, selection, mods, nil)
		require.ErrorContains(t, err, selection)
	}
}
