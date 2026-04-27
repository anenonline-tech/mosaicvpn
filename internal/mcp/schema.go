package mcp

import "encoding/json"

// schemaObject builds an MCP-compatible JSON Schema object describing the
// tool's input arguments.
func schemaObject(properties map[string]any, required []string) map[string]any {
	obj := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		obj["required"] = required
	}
	return obj
}

func schemaString(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func schemaBool(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

// schemaRawObject describes an argument that should be passed verbatim
// as a JSON object — the daemon decodes it into the corresponding Go
// struct on the server side.
func schemaRawObject(desc string) map[string]any {
	return map[string]any{"type": "object", "description": desc, "additionalProperties": true}
}

// jsonReencode marshals v then returns the resulting bytes; useful for
// converting an `any` decoded from JSON-RPC params back into bytes that
// can be Unmarshal'd into a typed struct.
func jsonReencode(v any) ([]byte, error) {
	return json.Marshal(v)
}

func jsonDecode(raw []byte, dst any) error {
	return json.Unmarshal(raw, dst)
}
