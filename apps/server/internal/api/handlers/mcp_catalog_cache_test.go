package handlers

import (
        "reflect"
        "testing"

        "github.com/stretchr/testify/require"
)

func requireSameMap(t *testing.T, a, b map[string]any) {
        t.Helper()
        require.NotNil(t, a)
        require.True(t, reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer(), "expected identical underlying map")
}

// The MCP catalog memoization must preserve exact lookup semantics while
// returning the same shared descriptors on every call. These tests pin the
// contract: stable identity, unique names, alias handling, and read-only
// scope filtering over the cached structures.

func TestMCPOperationCatalogIsMemoized(t *testing.T) {
        t.Parallel()

        first := mcpOperationCatalog()
        second := mcpOperationCatalog()
        require.NotEmpty(t, first)
        require.Same(t, &first[0], &second[0], "mcpOperationCatalog must return the same memoized slice on every call")
}

func TestMCPOperationCatalogNamesAreUnique(t *testing.T) {
        t.Parallel()

        seen := make(map[string]bool, len(mcpOperationCatalog()))
        for _, operation := range mcpOperationCatalog() {
                name, _ := operation.Descriptor["name"].(string)
                require.NotEmpty(t, name, "catalog operation descriptors must have names")
                require.False(t, seen[name], "duplicate catalog operation name %q", name)
                seen[name] = true
        }
        require.Equal(t, len(mcpOperationCatalog()), len(mcpCatalogIndex), "catalog index must cover every operation exactly once")
}

func TestMCPOperationByNameMatchesCatalogIndex(t *testing.T) {
        t.Parallel()

        // Canonical name resolves to the same shared descriptor.
        catalogName := ""
        for _, operation := range mcpOperationCatalog() {
                catalogName, _ = operation.Descriptor["name"].(string)
                break
        }
        byName, ok := mcpOperationByName(catalogName)
        require.True(t, ok, "canonical name %q must resolve", catalogName)
        require.Equal(t, catalogName, byName.Descriptor["name"])

        // Retired alias resolves to the canonical shared descriptor.
        byAlias, ok := mcpOperationByName("list_publications")
        require.True(t, ok, "retired alias list_publications must resolve to list_posts")
        require.Equal(t, "list_posts", byAlias.Descriptor["name"])
        canonical, ok := mcpOperationByName("list_posts")
        require.True(t, ok)
        requireSameMap(t, canonical.Descriptor, byAlias.Descriptor)

        // Unknown names still fail closed.
        _, ok = mcpOperationByName("definitely_not_a_real_operation")
        require.False(t, ok)
}

func TestMCPAdvertisedToolsAreMemoized(t *testing.T) {
        t.Parallel()

        first := mcpAdvertisedTools()
        second := mcpAdvertisedTools()
        require.NotEmpty(t, first)
        require.Same(t, &first[0], &second[0], "mcpAdvertisedTools must return the same memoized slice on every call")

        for _, tool := range first {
                name, _ := tool["name"].(string)
                require.NotEmpty(t, name)
                cached, ok := mcpAdvertisedIdx[name]
                require.True(t, ok, "advertised tool %q must be indexed", name)
                requireSameMap(t, tool, cached)
        }
}

func TestMCPToolByNamePrefersAdvertisedAndKeepsCatalogFallback(t *testing.T) {
        t.Parallel()

        // Advertised tool resolves from the memoized index.
        tool, ok := mcpToolByName("search_operations")
        require.True(t, ok)
        require.Equal(t, "search_operations", tool["name"])

        // Direct catalog tool resolves through the fallback path.
        post, ok := mcpToolByName("create_post")
        require.True(t, ok)
        require.Equal(t, "create_post", post["name"])

        // Retired alias still resolves to the canonical descriptor.
        aliased, ok := mcpToolByName("create_publication")
        require.True(t, ok)
        require.Equal(t, "create_post", aliased["name"])
        requireSameMap(t, post, aliased)

        _, ok = mcpToolByName("definitely_not_a_real_tool")
        require.False(t, ok)
}

func TestMCPScopeFilteringWorksOverMemoizedCatalog(t *testing.T) {
        t.Parallel()

        readOnlyTools := mcpDirectToolsForScope("mcp:read")
        require.NotEmpty(t, readOnlyTools)
        for _, tool := range readOnlyTools {
                name, _ := tool["name"].(string)
                require.NotEqual(t, "publish_post_now", name, "read-only scope must not see execute-mode tools")
        }

        fullTools := mcpDirectToolsForScope("mcp:full")
        require.Greater(t, len(fullTools), len(readOnlyTools), "full scope must expose strictly more tools than read-only scope")
}
