package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/shamith/mongodiff/pkg/diff"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorGray   = "\033[90m"
	colorBold   = "\033[1m"
)

// TerminalRenderer renders diff results with color-coded terminal output.
type TerminalRenderer struct{}

func NewTerminalRenderer() *TerminalRenderer {
	return &TerminalRenderer{}
}

func (r *TerminalRenderer) Render(w io.Writer, result *diff.DiffResult) error {
	// Header
	fmt.Fprintf(w, "\n%smongodiff%s — comparing %s → %s (database: %s)\n\n",
		colorBold, colorReset, result.Source, result.Target, result.Database)

	// Collections summary
	fmt.Fprintf(w, "%s━━━ Collections ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n\n", colorBold, colorReset)
	for _, coll := range result.Collections {
		r.renderCollectionSummary(w, coll)
	}
	fmt.Fprintln(w)

	// Per-collection detail sections (only for non-identical collections)
	for _, coll := range result.Collections {
		if coll.DiffType == "" {
			continue // identical, skip detail
		}
		r.renderCollectionDetail(w, coll)
	}

	// Summary footer
	r.renderSummary(w, result)

	return nil
}

func (r *TerminalRenderer) renderCollectionSummary(w io.Writer, coll diff.CollectionDiff) {
	switch coll.DiffType {
	case diff.Added:
		docCount := coll.Stats.DocumentsAdded
		fmt.Fprintf(w, "  %s+ %-30s (new collection, %d documents)%s\n",
			colorGreen, coll.Name, docCount, colorReset)
	case diff.Removed:
		docCount := coll.Stats.DocumentsRemoved
		fmt.Fprintf(w, "  %s- %-30s (removed collection, %d documents)%s\n",
			colorRed, coll.Name, docCount, colorReset)
	case diff.Modified:
		summary := r.modifiedCollectionSummary(coll)
		fmt.Fprintf(w, "  %s~ %-30s (%s)%s\n",
			colorYellow, coll.Name, summary, colorReset)
	default:
		// Identical
		fmt.Fprintf(w, "  %s  %-30s (identical)%s\n",
			colorGray, coll.Name, colorReset)
	}
}

func (r *TerminalRenderer) modifiedCollectionSummary(coll diff.CollectionDiff) string {
	var parts []string
	if coll.Stats.DocumentsModified > 0 {
		parts = append(parts, fmt.Sprintf("%d modified", coll.Stats.DocumentsModified))
	}
	if coll.Stats.DocumentsAdded > 0 {
		parts = append(parts, fmt.Sprintf("%d added", coll.Stats.DocumentsAdded))
	}
	if coll.Stats.DocumentsRemoved > 0 {
		parts = append(parts, fmt.Sprintf("%d removed", coll.Stats.DocumentsRemoved))
	}
	return strings.Join(parts, ", ")
}

func (r *TerminalRenderer) renderCollectionDetail(w io.Writer, coll diff.CollectionDiff) {
	label := ""
	switch coll.DiffType {
	case diff.Added:
		label = " (added)"
	case diff.Removed:
		label = " (removed)"
	}

	fmt.Fprintf(w, "%s━━━ Collection: %s%s ━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n\n",
		colorBold, coll.Name, label, colorReset)

	for _, doc := range coll.Documents {
		r.renderDocumentDiff(w, doc)
	}
	fmt.Fprintln(w)
}

func (r *TerminalRenderer) renderDocumentDiff(w io.Writer, doc diff.DocumentDiff) {
	idStr := diff.FormatValue(doc.ID)

	switch doc.DiffType {
	case diff.Added:
		fmt.Fprintf(w, "  %s+ _id: %s%s\n", colorGreen, idStr, colorReset)
		r.renderDocumentFields(w, doc.Source, "    ", colorGreen)
	case diff.Removed:
		fmt.Fprintf(w, "  %s- _id: %s%s\n", colorRed, idStr, colorReset)
		r.renderDocumentFields(w, doc.Target, "    ", colorRed)
	case diff.Modified:
		fmt.Fprintf(w, "  %s~ _id: %s%s\n", colorYellow, idStr, colorReset)
		for _, field := range doc.Fields {
			r.renderFieldDiff(w, field)
		}
	}
	fmt.Fprintln(w)
}

func (r *TerminalRenderer) renderDocumentFields(w io.Writer, doc map[string]interface{}, indent, color string) {
	if doc == nil {
		return
	}
	// Show abbreviated document content
	parts := []string{}
	for k, v := range doc {
		if k == "_id" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", k, diff.FormatValue(v)))
	}
	if len(parts) > 0 {
		summary := strings.Join(parts, ", ")
		if len(summary) > 80 {
			summary = summary[:77] + "..."
		}
		fmt.Fprintf(w, "%s%s{ %s }%s\n", indent, color, summary, colorReset)
	}
}

func (r *TerminalRenderer) renderFieldDiff(w io.Writer, field diff.FieldDiff) {
	switch field.DiffType {
	case diff.Added:
		fmt.Fprintf(w, "    %s+ %s: %s%s\n",
			colorGreen, field.Path, diff.FormatValue(field.NewValue), colorReset)
	case diff.Removed:
		fmt.Fprintf(w, "    %s- %s: %s%s\n",
			colorRed, field.Path, diff.FormatValue(field.OldValue), colorReset)
	case diff.Modified:
		oldType := diff.BSONTypeName(field.OldValue)
		newType := diff.BSONTypeName(field.NewValue)
		if oldType != newType {
			// Type change — show types per Rule 1
			fmt.Fprintf(w, "    %s~ %s: (%s → %s) %s → %s%s\n",
				colorYellow, field.Path, oldType, newType,
				diff.FormatValue(field.OldValue), diff.FormatValue(field.NewValue), colorReset)
		} else {
			fmt.Fprintf(w, "    %s- %s: %s%s\n",
				colorRed, field.Path, diff.FormatValue(field.OldValue), colorReset)
			fmt.Fprintf(w, "    %s+ %s: %s%s\n",
				colorGreen, field.Path, diff.FormatValue(field.NewValue), colorReset)
		}
	}
}

func (r *TerminalRenderer) renderSummary(w io.Writer, result *diff.DiffResult) {
	fmt.Fprintf(w, "%s━━━ Summary ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n\n", colorBold, colorReset)

	// Collection stats
	collParts := []string{}
	if result.Stats.CollectionsAdded > 0 {
		collParts = append(collParts, fmt.Sprintf("%d collection added", result.Stats.CollectionsAdded))
	}
	if result.Stats.CollectionsRemoved > 0 {
		collParts = append(collParts, fmt.Sprintf("%d collection removed", result.Stats.CollectionsRemoved))
	}
	modifiedColls := 0
	identicalColls := 0
	for _, c := range result.Collections {
		if c.DiffType == diff.Modified {
			modifiedColls++
		} else if c.DiffType == "" {
			identicalColls++
		}
	}
	if modifiedColls > 0 {
		word := "collections"
		if modifiedColls == 1 {
			word = "collection"
		}
		collParts = append(collParts, fmt.Sprintf("%d %s modified", modifiedColls, word))
	}
	if identicalColls > 0 {
		collParts = append(collParts, fmt.Sprintf("%d identical", identicalColls))
	}
	if len(collParts) > 0 {
		fmt.Fprintf(w, "  %s\n", strings.Join(collParts, ", "))
	}

	// Document stats
	docParts := []string{}
	if result.Stats.DocumentsAdded > 0 {
		docParts = append(docParts, fmt.Sprintf("%d documents added", result.Stats.DocumentsAdded))
	}
	if result.Stats.DocumentsModified > 0 {
		docParts = append(docParts, fmt.Sprintf("%d documents modified", result.Stats.DocumentsModified))
	}
	if result.Stats.DocumentsRemoved > 0 {
		docParts = append(docParts, fmt.Sprintf("%d documents removed", result.Stats.DocumentsRemoved))
	}
	if len(docParts) > 0 {
		fmt.Fprintf(w, "  %s\n", strings.Join(docParts, ", "))
	}

	fmt.Fprintln(w)
}
