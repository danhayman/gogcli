package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"google.golang.org/api/docs/v1"

	"github.com/steipete/gogcli/internal/outfmt"
	"github.com/steipete/gogcli/internal/ui"
)

// DocsEditCmd replaces, inserts, or deletes text in a Google Doc.
type DocsEditCmd struct {
	DocID      string `arg:"" name:"docId" help:"Google Doc ID or URL"`
	Old        string `name:"old" required:"" help:"Text to find (must be unique unless --replace-all)"`
	New        string `name:"new" required:"" help:"Replacement text (use '' to delete, use \\n to insert paragraphs)"`
	ReplaceAll bool   `name:"replace-all" help:"Replace all occurrences (required if --old matches more than once)"`
	MatchCase  bool   `name:"match-case" help:"Case-sensitive matching (use --no-match-case for case-insensitive)" default:"true" negatable:""`
}

func (c *DocsEditCmd) Run(ctx context.Context, flags *RootFlags) error {
	u := ui.FromContext(ctx)
	account, err := requireAccount(flags)
	if err != nil {
		return err
	}

	docID := strings.TrimSpace(c.DocID)
	if docID == "" {
		return usage("empty docId")
	}
	oldText := unescapeString(c.Old)
	newText := unescapeString(c.New)

	if oldText == "" {
		return usage("--old cannot be empty")
	}

	svc, err := newDocsService(ctx, account)
	if err != nil {
		return err
	}

	// Fetch document text to validate uniqueness.
	doc, err := svc.Documents.Get(docID).
		Context(ctx).
		Do()
	if err != nil {
		if isDocsNotFound(err) {
			return fmt.Errorf("doc not found or not a Google Doc (id=%s)", docID)
		}
		return err
	}
	if doc == nil {
		return errors.New("doc not found")
	}

	plainText := docsPlainText(doc, 0)
	occurrences := countOccurrences(plainText, oldText, c.MatchCase)

	if occurrences == 0 {
		return fmt.Errorf("%q not found", oldText)
	}
	if !c.ReplaceAll && occurrences > 1 {
		return fmt.Errorf("%q is not unique (found %d occurrences). Use --replace-all to replace all.", oldText, occurrences)
	}

	if flags.DryRun {
		if outfmt.IsJSON(ctx) {
			return outfmt.WriteJSON(ctx, os.Stdout, map[string]any{
				"dry_run":     true,
				"documentId":  docID,
				"old":         oldText,
				"new":         newText,
				"occurrences": occurrences,
			})
		}
		u.Out().Printf("would replace %d occurrence%s", occurrences, plural(occurrences))
		return nil
	}

	result, err := svc.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
		Requests: []*docs.Request{{
			ReplaceAllText: &docs.ReplaceAllTextRequest{
				ContainsText: &docs.SubstringMatchCriteria{
					Text:      oldText,
					MatchCase: c.MatchCase,
				},
				ReplaceText: newText,
			},
		}},
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("edit document: %w", err)
	}

	replacements := int64(0)
	if len(result.Replies) > 0 && result.Replies[0].ReplaceAllText != nil {
		replacements = result.Replies[0].ReplaceAllText.OccurrencesChanged
	}

	if outfmt.IsJSON(ctx) {
		return outfmt.WriteJSON(ctx, os.Stdout, map[string]any{
			"documentId":   result.DocumentId,
			"old":          oldText,
			"new":          newText,
			"replacements": replacements,
			"matchCase":    c.MatchCase,
		})
	}

	u.Out().Printf("replaced %d occurrence%s", replacements, plural(int(replacements)))
	return nil
}
