package astanalyzer

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFormatMatchesV1(t *testing.T) {
	t.Run("empty results", func(t *testing.T) { assert.Empty(t, formatMatchesV1(nil)) })
	t.Run("recognized path retains evidence and hierarchy", func(t *testing.T) {
		match := AnalyzerMatch{FilePath: "app.js", AnalyzerName: "robust-paths", Value: "'/api/users'", Tags: map[string]bool{"is-path-only": true}}
		roots := formatMatchesV1([]AnalyzerMatch{match})
		require.Len(t, roots, 1)
		assert.Equal(t, "Data", roots[0].Label)
		assert.Equal(t, "[1]", roots[0].Description)
		require.Len(t, roots[0].Children, 1)
		paths := roots[0].Children[0]
		assert.Equal(t, "Paths", paths.Label)
		require.Len(t, paths.Children, 1)
		require.Len(t, paths.Children[0].Children, 1)
		leaf := paths.Children[0].Children[0]
		assert.Equal(t, "/api/users", leaf.Label)
		assert.Equal(t, match, leaf.Data)
	})
	t.Run("unrecognized tags do not invent categories", func(t *testing.T) {
		assert.Empty(t, formatMatchesV1([]AnalyzerMatch{{Value: "unknown", Tags: map[string]bool{"unknown": true}}}))
	})
	t.Run("multiple analyzer categories keep all recognized evidence", func(t *testing.T) {
		matches := []AnalyzerMatch{
			{Value: "/api/users", Tags: map[string]bool{"is-path-only": true}},
			{Value: "postMessage('x')", Tags: map[string]bool{"postMessage": true}},
		}
		var leaves []string
		var visit func(ASTAnalyzerTreeNode)
		visit = func(n ASTAnalyzerTreeNode) {
			if n.Type == ASTAnalyzerTreeNodeTypeMatch {
				leaves = append(leaves, n.Label)
			}
			for _, c := range n.Children {
				visit(c)
			}
		}
		for _, n := range formatMatchesV1(matches) {
			visit(n)
		}
		assert.ElementsMatch(t, []string{"/api/users", "postMessage('x')"}, leaves)
	})
}
