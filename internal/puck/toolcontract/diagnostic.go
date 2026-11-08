package toolcontract

import (
	"bytes"
	"encoding/json"
)

const (
	maxShapeDepth = 6
	maxShapeKeys  = 32
	maxShapeItems = 8
)

type ArgumentShape struct {
	Type      string                   `json:"type"`
	Length    int                      `json:"length,omitempty"`
	Fields    map[string]ArgumentShape `json:"fields,omitempty"`
	Items     []ArgumentShape          `json:"items,omitempty"`
	Truncated bool                     `json:"truncated,omitempty"`
}

type Diagnostic struct {
	Tool    string        `json:"tool"`
	Details []FieldError  `json:"details,omitempty"`
	Shape   ArgumentShape `json:"shape"`
}

// RejectionDiagnostic describes argument structure without retaining scalar
// values. Object keys are intentionally preserved because they are required to
// diagnose schema drift; content, identifiers and numeric/boolean values are not.
func RejectionDiagnostic(tool string, raw json.RawMessage, details []FieldError) Diagnostic {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return Diagnostic{Tool: tool, Details: append([]FieldError(nil), details...), Shape: ArgumentShape{Type: "invalid_json"}}
	}
	shape := shapeOf(value, 0)
	if encoded, _ := json.Marshal(shape); len(encoded) > 4096 {
		shape = shallowShape(shape)
	}
	return Diagnostic{Tool: tool, Details: append([]FieldError(nil), details...), Shape: shape}
}

func shallowShape(shape ArgumentShape) ArgumentShape {
	bounded := ArgumentShape{Type: shape.Type, Length: shape.Length, Truncated: true}
	if len(shape.Fields) != 0 {
		bounded.Fields = make(map[string]ArgumentShape, min(len(shape.Fields), maxShapeKeys))
		count := 0
		for key, child := range shape.Fields {
			if count >= maxShapeKeys {
				break
			}
			bounded.Fields[key] = ArgumentShape{Type: child.Type, Length: child.Length, Truncated: len(child.Fields) != 0 || len(child.Items) != 0}
			count++
		}
	}
	return bounded
}

func shapeOf(value any, depth int) ArgumentShape {
	if depth >= maxShapeDepth {
		return ArgumentShape{Type: jsonType(value), Truncated: true}
	}
	switch typed := value.(type) {
	case map[string]any:
		shape := ArgumentShape{Type: "object", Length: len(typed), Fields: map[string]ArgumentShape{}}
		count := 0
		for key, child := range typed {
			if count >= maxShapeKeys {
				shape.Truncated = true
				break
			}
			shape.Fields[key] = shapeOf(child, depth+1)
			count++
		}
		return shape
	case []any:
		shape := ArgumentShape{Type: "array", Length: len(typed)}
		limit := len(typed)
		if limit > maxShapeItems {
			limit = maxShapeItems
			shape.Truncated = true
		}
		shape.Items = make([]ArgumentShape, 0, limit)
		for _, child := range typed[:limit] {
			shape.Items = append(shape.Items, shapeOf(child, depth+1))
		}
		return shape
	default:
		return ArgumentShape{Type: jsonType(value)}
	}
}

func jsonType(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number, float64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}
