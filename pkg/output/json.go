package output

import (
	"encoding/json"
	"io"

	"github.com/shamith/mongodiff/pkg/diff"
)

// jsonDiffResult is the JSON-serializable version of DiffResult.
type jsonDiffResult struct {
	Source      string               `json:"source"`
	Target      string               `json:"target"`
	Database    string               `json:"database"`
	Timestamp   string               `json:"timestamp"`
	Collections []jsonCollectionDiff  `json:"collections"`
	Stats       diff.DiffStats        `json:"stats"`
}

type jsonCollectionDiff struct {
	Name      string             `json:"name"`
	DiffType  string             `json:"diffType"`
	Documents []jsonDocumentDiff  `json:"documents,omitempty"`
	Stats     diff.DiffStats      `json:"stats"`
	Error     string             `json:"error,omitempty"`
}

type jsonDocumentDiff struct {
	ID       interface{}      `json:"_id"`
	DiffType string           `json:"diffType"`
	Fields   []jsonFieldDiff  `json:"fields,omitempty"`
	Source   interface{}      `json:"source,omitempty"`
	Target   interface{}      `json:"target,omitempty"`
}

type jsonFieldDiff struct {
	Path     string      `json:"path"`
	DiffType string      `json:"diffType"`
	OldValue interface{} `json:"oldValue,omitempty"`
	NewValue interface{} `json:"newValue,omitempty"`
	OldType  string      `json:"oldType,omitempty"`
	NewType  string      `json:"newType,omitempty"`
}

// JSONRenderer renders diff results as JSON.
type JSONRenderer struct {
	SummaryOnly bool
}

func NewJSONRenderer() *JSONRenderer {
	return &JSONRenderer{}
}

func (r *JSONRenderer) Render(w io.Writer, result *diff.DiffResult) error {
	out := jsonDiffResult{
		Source:    result.Source,
		Target:    result.Target,
		Database:  result.Database,
		Timestamp: result.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z"),
		Stats:     result.Stats,
	}

	for _, coll := range result.Collections {
		jc := jsonCollectionDiff{
			Name:     coll.Name,
			DiffType: collDiffType(coll),
			Stats:    coll.Stats,
			Error:    coll.Error,
		}

		if !r.SummaryOnly {
			for _, doc := range coll.Documents {
				jd := jsonDocumentDiff{
					ID:       formatIDForJSON(doc.ID),
					DiffType: string(doc.DiffType),
				}

				if doc.Source != nil {
					jd.Source = doc.Source
				}
				if doc.Target != nil {
					jd.Target = doc.Target
				}

				for _, field := range doc.Fields {
					jf := jsonFieldDiff{
						Path:     field.Path,
						DiffType: string(field.DiffType),
					}
					if field.OldValue != nil {
						jf.OldValue = diff.FormatValue(field.OldValue)
						jf.OldType = diff.BSONTypeName(field.OldValue)
					}
					if field.NewValue != nil {
						jf.NewValue = diff.FormatValue(field.NewValue)
						jf.NewType = diff.BSONTypeName(field.NewValue)
					}
					jd.Fields = append(jd.Fields, jf)
				}

				jc.Documents = append(jc.Documents, jd)
			}
		}

		out.Collections = append(out.Collections, jc)
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}

func collDiffType(coll diff.CollectionDiff) string {
	if coll.Error != "" {
		return "error"
	}
	if coll.DiffType == "" {
		return "identical"
	}
	return string(coll.DiffType)
}

func formatIDForJSON(id interface{}) interface{} {
	return diff.FormatValue(id)
}
