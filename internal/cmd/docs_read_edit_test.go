package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/docs/v1"
	"google.golang.org/api/option"

	"github.com/steipete/gogcli/internal/outfmt"
	"github.com/steipete/gogcli/internal/ui"
)

// newReadEditTestServer creates a mock Docs API server for read/edit tests.
// It handles GET (document fetch) and POST (batchUpdate) requests.
func newReadEditTestServer(t *testing.T, handler http.HandlerFunc) (*docs.Service, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	docSvc, err := docs.NewService(context.Background(),
		option.WithoutAuthentication(),
		option.WithHTTPClient(srv.Client()),
		option.WithEndpoint(srv.URL+"/"),
	)
	require.NoError(t, err)
	return docSvc, srv.Close
}

func docResponse(content string) map[string]any {
	return map[string]any{
		"documentId": "doc1",
		"body": map[string]any{
			"content": []any{
				map[string]any{
					"paragraph": map[string]any{
						"elements": []any{
							map[string]any{
								"textRun": map[string]any{"content": content},
							},
						},
					},
				},
			},
		},
	}
}

func readEditContext(t *testing.T) context.Context {
	t.Helper()
	u, err := ui.New(ui.Options{Stdout: io.Discard, Stderr: io.Discard, Color: "never"})
	require.NoError(t, err)
	return ui.WithUI(context.Background(), u)
}

func readEditContextWithStdout(t *testing.T) context.Context {
	t.Helper()
	u, err := ui.New(ui.Options{Stdout: os.Stdout, Stderr: io.Discard, Color: "never"})
	require.NoError(t, err)
	return ui.WithUI(context.Background(), u)
}

// --- Helper unit tests ---

func TestApplyCharWindow(t *testing.T) {
	text := "abcdefghij" // 10 chars

	tests := []struct {
		name   string
		offset int
		limit  int
		want   string
	}{
		{"no window", 0, 0, "abcdefghij"},
		{"offset only", 3, 0, "defghij"},
		{"limit only", 0, 4, "abcd"},
		{"offset and limit", 2, 3, "cde"},
		{"offset past end", 15, 0, ""},
		{"limit past end", 8, 100, "ij"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyCharWindow(text, tt.offset, tt.limit)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCountOccurrences(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		substr    string
		matchCase bool
		want      int
	}{
		{"exact match", "hello world hello", "hello", true, 2},
		{"case sensitive miss", "Hello world hello", "hello", true, 1},
		{"case insensitive", "Hello world hello", "hello", false, 2},
		{"not found", "abc", "xyz", true, 0},
		{"empty text", "", "hello", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, countOccurrences(tt.text, tt.substr, tt.matchCase))
		})
	}
}

func TestUnescapeString(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no escapes", "hello world", "hello world"},
		{"newline", `hello\nworld`, "hello\nworld"},
		{"tab", `hello\tworld`, "hello\tworld"},
		{"backslash", `hello\\world`, "hello\\world"},
		{"multiple", `a\nb\tc\\d`, "a\nb\tc\\d"},
		{"unknown escape kept", `hello\xworld`, `hello\xworld`},
		{"trailing backslash", `hello\`, `hello\`},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, unescapeString(tt.in))
		})
	}
}

// --- DocsReadCmd tests ---

func TestDocsRead_PlainText(t *testing.T) {
	docContent := "hello world"
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(docResponse(docContent))
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com"}
	out := captureStdout(t, func() {
		cmd := &DocsReadCmd{}
		err := runKong(t, cmd, []string{"doc1"}, readEditContextWithStdout(t), flags)
		require.NoError(t, err)
	})
	assert.Equal(t, docContent, out)
}

func TestDocsRead_CharOffset(t *testing.T) {
	docContent := "abcdefghij" // 10 chars
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(docResponse(docContent))
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com"}
	// --offset 3 --limit 4: chars at index 3..6 = "defg"
	out := captureStdout(t, func() {
		cmd := &DocsReadCmd{}
		err := runKong(t, cmd, []string{"doc1", "--offset", "3", "--limit", "4"}, readEditContextWithStdout(t), flags)
		require.NoError(t, err)
	})
	assert.Equal(t, "defg", out)
}

func TestDocsRead_OffsetOnly(t *testing.T) {
	docContent := "abcdefghij"
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(docResponse(docContent))
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com"}
	// --offset 7 --limit 0: chars from index 7 onward = "hij"
	out := captureStdout(t, func() {
		cmd := &DocsReadCmd{}
		err := runKong(t, cmd, []string{"doc1", "--offset", "7", "--limit", "0"}, readEditContextWithStdout(t), flags)
		require.NoError(t, err)
	})
	assert.Equal(t, "hij", out)
}

func TestDocsRead_LimitOnly(t *testing.T) {
	docContent := "abcdefghij"
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(docResponse(docContent))
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com"}
	// --offset 0 --limit 5: first 5 chars = "abcde"
	out := captureStdout(t, func() {
		cmd := &DocsReadCmd{}
		err := runKong(t, cmd, []string{"doc1", "--offset", "0", "--limit", "5"}, readEditContextWithStdout(t), flags)
		require.NoError(t, err)
	})
	assert.Equal(t, "abcde", out)
}

func TestDocsRead_OffsetPastEnd(t *testing.T) {
	docContent := "short"
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(docResponse(docContent))
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com"}
	out := captureStdout(t, func() {
		cmd := &DocsReadCmd{}
		err := runKong(t, cmd, []string{"doc1", "--offset", "999", "--limit", "0"}, readEditContextWithStdout(t), flags)
		require.NoError(t, err)
	})
	assert.Equal(t, "", out)
}

func TestDocsRead_JSON(t *testing.T) {
	docContent := "hello world" // 11 chars
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(docResponse(docContent))
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com"}
	ctx := outfmt.WithMode(readEditContextWithStdout(t), outfmt.Mode{JSON: true})

	out := captureStdout(t, func() {
		cmd := &DocsReadCmd{}
		err := runKong(t, cmd, []string{"doc1", "--offset", "6", "--limit", "5"}, ctx, flags)
		require.NoError(t, err)
	})

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, "world", result["text"])
	assert.Equal(t, float64(11), result["totalChars"])
}

// --- DocsEditCmd tests ---

func TestDocsEdit_UniqueReplacement(t *testing.T) {
	var gotBatch docs.BatchUpdateDocumentRequest
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(docResponse("hello foo world"))
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&gotBatch)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"documentId": "doc1",
				"replies":    []any{map[string]any{"replaceAllText": map[string]any{"occurrencesChanged": 1}}},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com"}
	out := captureStdout(t, func() {
		cmd := &DocsEditCmd{}
		err := runKong(t, cmd, []string{"doc1", "--old", "foo", "--new", "bar"}, readEditContextWithStdout(t), flags)
		require.NoError(t, err)
	})

	assert.Contains(t, out, "replaced 1 occurrence")
	require.Len(t, gotBatch.Requests, 1)
	require.NotNil(t, gotBatch.Requests[0].ReplaceAllText)
	r := gotBatch.Requests[0].ReplaceAllText
	assert.Equal(t, "foo", r.ContainsText.Text)
	assert.Equal(t, "bar", r.ReplaceText)
}

func TestDocsEdit_NotUnique_Error(t *testing.T) {
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(docResponse("foo bar foo baz foo"))
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com"}
	cmd := &DocsEditCmd{}
	err := runKong(t, cmd, []string{"doc1", "--old", "foo", "--new", "bar"}, readEditContext(t), flags)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not unique")
	assert.Contains(t, err.Error(), "3 occurrences")
}

func TestDocsEdit_NotFound_Error(t *testing.T) {
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(docResponse("hello world"))
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com"}
	cmd := &DocsEditCmd{}
	err := runKong(t, cmd, []string{"doc1", "--old", "xyz", "--new", "abc"}, readEditContext(t), flags)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDocsEdit_ReplaceAll(t *testing.T) {
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(docResponse("foo bar foo baz foo"))
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"documentId": "doc1",
				"replies":    []any{map[string]any{"replaceAllText": map[string]any{"occurrencesChanged": 3}}},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com"}
	out := captureStdout(t, func() {
		cmd := &DocsEditCmd{}
		err := runKong(t, cmd, []string{"doc1", "--old", "foo", "--new", "bar", "--replace-all"}, readEditContextWithStdout(t), flags)
		require.NoError(t, err)
	})
	assert.Contains(t, out, "replaced 3 occurrences")
}

func TestDocsEdit_JSON(t *testing.T) {
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(docResponse("hello foo world"))
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"documentId": "doc1",
				"replies":    []any{map[string]any{"replaceAllText": map[string]any{"occurrencesChanged": 1}}},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com"}
	ctx := outfmt.WithMode(readEditContextWithStdout(t), outfmt.Mode{JSON: true})

	out := captureStdout(t, func() {
		cmd := &DocsEditCmd{}
		err := runKong(t, cmd, []string{"doc1", "--old", "foo", "--new", "bar"}, ctx, flags)
		require.NoError(t, err)
	})

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, "foo", result["old"])
	assert.Equal(t, "bar", result["new"])
	assert.Equal(t, float64(1), result["replacements"])
}

func TestDocsEdit_DryRun(t *testing.T) {
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			t.Fatal("dry-run should not call batchUpdate")
		}
		_ = json.NewEncoder(w).Encode(docResponse("hello foo world"))
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com", DryRun: true}
	out := captureStdout(t, func() {
		cmd := &DocsEditCmd{}
		err := runKong(t, cmd, []string{"doc1", "--old", "foo", "--new", "bar"}, readEditContextWithStdout(t), flags)
		require.NoError(t, err)
	})
	assert.Contains(t, out, "would replace 1 occurrence")
}

func TestDocsEdit_DryRun_JSON(t *testing.T) {
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			t.Fatal("dry-run should not call batchUpdate")
		}
		_ = json.NewEncoder(w).Encode(docResponse("foo bar foo"))
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com", DryRun: true}
	ctx := outfmt.WithMode(readEditContextWithStdout(t), outfmt.Mode{JSON: true})

	out := captureStdout(t, func() {
		cmd := &DocsEditCmd{}
		err := runKong(t, cmd, []string{"doc1", "--old", "foo", "--new", "baz", "--replace-all"}, ctx, flags)
		require.NoError(t, err)
	})

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, true, result["dry_run"])
	assert.Equal(t, float64(2), result["occurrences"])
	assert.Equal(t, "foo", result["old"])
	assert.Equal(t, "baz", result["new"])
}

func TestDocsEdit_Deletion(t *testing.T) {
	var gotBatch docs.BatchUpdateDocumentRequest
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(docResponse("hello removeme world"))
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&gotBatch)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"documentId": "doc1",
				"replies":    []any{map[string]any{"replaceAllText": map[string]any{"occurrencesChanged": 1}}},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com"}
	out := captureStdout(t, func() {
		cmd := &DocsEditCmd{}
		err := runKong(t, cmd, []string{"doc1", "--old", "removeme", "--new", ""}, readEditContextWithStdout(t), flags)
		require.NoError(t, err)
	})
	assert.Contains(t, out, "replaced 1 occurrence")
	assert.Equal(t, "", gotBatch.Requests[0].ReplaceAllText.ReplaceText)
}

func TestDocsEdit_EscapeSequences(t *testing.T) {
	var gotBatch docs.BatchUpdateDocumentRequest
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(docResponse("hello world"))
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&gotBatch)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"documentId": "doc1",
				"replies":    []any{map[string]any{"replaceAllText": map[string]any{"occurrencesChanged": 1}}},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com"}
	captureStdout(t, func() {
		cmd := &DocsEditCmd{}
		// \n in --new should be unescaped to actual newline
		err := runKong(t, cmd, []string{"doc1", "--old", "hello world", "--new", `hello\nworld`}, readEditContextWithStdout(t), flags)
		require.NoError(t, err)
	})
	r := gotBatch.Requests[0].ReplaceAllText
	assert.Equal(t, "hello\nworld", r.ReplaceText)
}

func TestDocsEdit_NoMatchCase(t *testing.T) {
	var gotBatch docs.BatchUpdateDocumentRequest
	svc, cleanup := newReadEditTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(docResponse("Hello HELLO hello"))
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&gotBatch)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"documentId": "doc1",
				"replies":    []any{map[string]any{"replaceAllText": map[string]any{"occurrencesChanged": 3}}},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer cleanup()
	mockDocsService(t, svc)

	flags := &RootFlags{Account: "a@b.com"}
	out := captureStdout(t, func() {
		cmd := &DocsEditCmd{}
		err := runKong(t, cmd, []string{"doc1", "--old", "hello", "--new", "hi", "--no-match-case", "--replace-all"}, readEditContextWithStdout(t), flags)
		require.NoError(t, err)
	})
	assert.Contains(t, out, "replaced 3 occurrences")
	assert.False(t, gotBatch.Requests[0].ReplaceAllText.ContainsText.MatchCase)
}
