package protocol

import (
	"bytes"
	"embed"
	"errors"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed v1.schema.json
var schemaFS embed.FS

var (
	compiledOnce sync.Once
	compiled     *jsonschema.Schema
	compileErr   error
)

func schema() (*jsonschema.Schema, error) {
	compiledOnce.Do(func() {
		body, err := schemaFS.ReadFile("v1.schema.json")
		if err != nil {
			compileErr = err
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
		if err != nil {
			compileErr = err
			return
		}
		compiler := jsonschema.NewCompiler()
		const uri = "https://github.com/wu8685/Ariel/protocol/v1.schema.json"
		if err := compiler.AddResource(uri, doc); err != nil {
			compileErr = err
			return
		}
		compiled, compileErr = compiler.Compile(uri)
	})
	return compiled, compileErr
}

// Validate checks one complete JSON message against the shared v1 schema.
func Validate(raw []byte) error {
	if len(raw) == 0 || len(raw) > 8<<20 {
		return errors.New("protocol frame exceeds bounds")
	}
	sch, err := schema()
	if err != nil {
		return err
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	return sch.Validate(value)
}
