package history

import (
	"fmt"
	"strings"
)

// ExportMarkdown generates a human-readable diff report from entries.
func ExportMarkdown(entries []Entry) string {
	var b strings.Builder
	b.WriteString("# Sync Report\n\n")

	for _, e := range entries {
		b.WriteString(fmt.Sprintf("**Applied:** %s\n", e.Timestamp.Format("January 2, 2006 at 3:04 PM")))
		b.WriteString(fmt.Sprintf("**Source:** %s → **Target:** %s\n", e.Source, e.Target))
		b.WriteString(fmt.Sprintf("**Database:** %s\n\n", e.Database))

		groups := groupByCollection(e.Operations)
		for _, g := range groups {
			b.WriteString(fmt.Sprintf("## %s\n\n", g.name))
			for _, op := range g.ops {
				b.WriteString(fmt.Sprintf("### `%v` — %s\n\n", op.DocID, op.Type))
				if len(op.Fields) > 0 {
					b.WriteString("| Field | Source | Target |\n")
					b.WriteString("|-------|--------|--------|\n")
					for _, f := range op.Fields {
						old := f.OldValue
						if old == "" {
							old = "_(absent)_"
						}
						new := f.NewValue
						if new == "" {
							new = "_(absent)_"
						}
						b.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", f.Path, old, new))
					}
					b.WriteString("\n")
				} else if op.Type == "insert" {
					b.WriteString("_New document inserted from source_\n\n")
				} else if op.Type == "delete" {
					b.WriteString("_Document deleted from target_\n\n")
				}
			}
		}

		b.WriteString("---\n")
		b.WriteString(fmt.Sprintf("**Summary:** %d inserted, %d replaced, %d deleted\n",
			e.Summary.Inserted, e.Summary.Replaced, e.Summary.Deleted))
		if e.BackupPath != "" {
			b.WriteString(fmt.Sprintf("**Backup:** %s\n", e.BackupPath))
		}
		b.WriteString("\n")
	}

	return b.String()
}

type collectionGroup struct {
	name string
	ops  []Operation
}

func groupByCollection(ops []Operation) []collectionGroup {
	orderMap := map[string]int{}
	var groups []collectionGroup

	for _, op := range ops {
		idx, ok := orderMap[op.Collection]
		if !ok {
			idx = len(groups)
			orderMap[op.Collection] = idx
			groups = append(groups, collectionGroup{name: op.Collection})
		}
		groups[idx].ops = append(groups[idx].ops, op)
	}
	return groups
}
