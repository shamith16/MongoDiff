package history

import (
	"fmt"
	"strings"
)

// ExportMarkdown generates a human-readable migration guide from entries.
func ExportMarkdown(entries []Entry) string {
	var b strings.Builder
	b.WriteString("# Migration Guide\n\n")

	for _, e := range entries {
		b.WriteString(fmt.Sprintf("**Applied:** %s\n", e.Timestamp.Format("January 2, 2006 at 3:04 PM")))
		b.WriteString(fmt.Sprintf("**Source:** %s → **Target:** %s\n", e.Source, e.Target))
		b.WriteString(fmt.Sprintf("**Database:** %s\n\n", e.Database))

		groups := groupByCollection(e.Operations)
		for _, g := range groups {
			b.WriteString(fmt.Sprintf("## %s (%d operation%s)\n", g.name, g.total, plural(g.total)))
			if len(g.inserts) > 0 {
				b.WriteString("- **Inserted:** " + formatIDs(g.inserts) + "\n")
			}
			if len(g.replaces) > 0 {
				b.WriteString("- **Replaced:** " + formatIDs(g.replaces) + "\n")
			}
			if len(g.deletes) > 0 {
				b.WriteString("- **Deleted:** " + formatIDs(g.deletes) + "\n")
			}
			b.WriteString("\n")
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

// ExportMongosh generates a mongosh script from entries.
// Delete operations are fully executable. Insert/replace operations include
// TODO comments since the log stores IDs only, not full document data.
func ExportMongosh(entries []Entry) string {
	var b strings.Builder
	b.WriteString("// mongodiff migration script\n")

	for _, e := range entries {
		b.WriteString(fmt.Sprintf("// Applied: %s\n", e.Timestamp.Format("2006-01-02T15:04:05Z")))
		b.WriteString(fmt.Sprintf("// Source: %s → Target: %s\n\n", e.Source, e.Target))
		b.WriteString(fmt.Sprintf("use(\"%s\");\n\n", e.Database))

		groups := groupByCollection(e.Operations)
		for _, g := range groups {
			b.WriteString(fmt.Sprintf("// --- %s ---\n", g.name))
			for _, id := range g.inserts {
				b.WriteString(fmt.Sprintf("db.%s.insertOne({_id: %s, /* TODO: fetch document from source */});\n",
					g.name, formatMongoshID(id)))
			}
			for _, id := range g.replaces {
				b.WriteString(fmt.Sprintf("db.%s.replaceOne({_id: %s}, {/* TODO: fetch document from source */});\n",
					g.name, formatMongoshID(id)))
			}
			for _, id := range g.deletes {
				b.WriteString(fmt.Sprintf("db.%s.deleteOne({_id: %s});\n",
					g.name, formatMongoshID(id)))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

type collectionGroup struct {
	name     string
	inserts  []interface{}
	replaces []interface{}
	deletes  []interface{}
	total    int
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
		g := &groups[idx]
		switch op.Type {
		case "insert":
			g.inserts = append(g.inserts, op.DocID)
		case "replace":
			g.replaces = append(g.replaces, op.DocID)
		case "delete":
			g.deletes = append(g.deletes, op.DocID)
		}
		g.total++
	}
	return groups
}

func formatIDs(ids []interface{}) string {
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = fmt.Sprintf("`%v`", id)
	}
	return strings.Join(strs, ", ")
}

func formatMongoshID(id interface{}) string {
	s := fmt.Sprintf("%v", id)
	// 24-char hex strings are likely ObjectIds
	if len(s) == 24 && isHex(s) {
		return fmt.Sprintf("ObjectId(\"%s\")", s)
	}
	return fmt.Sprintf("\"%s\"", s)
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
