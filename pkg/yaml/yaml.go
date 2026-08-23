package yaml

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

func Unmarshal(in []byte, out any) (err error) {
	return yaml.Unmarshal(in, out)
}

func Encode(v any, indent int) ([]byte, error) {
	b := bytes.NewBuffer(nil)
	e := yaml.NewEncoder(b)
	e.SetIndent(indent)

	if err := e.Encode(v); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

func Patch(in []byte, path []string, value any) ([]byte, error) {
	out, err := patch(in, path, value)
	if err != nil {
		return nil, err
	}

	// validate
	if err = yaml.Unmarshal(out, map[string]any{}); err != nil {
		return nil, err
	}

	return out, nil
}

func patch(in []byte, path []string, value any) ([]byte, error) {
	if len(path) == 0 {
		return in, nil
	}

	var root yaml.Node
	if err := yaml.Unmarshal(in, &root); err != nil {
		// invalid yaml
		return nil, err
	}

	// empty in
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		if value == nil {
			return in, nil
		}
		paste, err := Encode(buildNestedMap(path, value), 2)
		if err != nil {
			return nil, err
		}
		return join(in, paste), nil
	}

	nodes := root.Content[0].Content
	n := len(path) - 1

	// Check if the exact leaf key already exists
	leafKey, _ := findNode(nodes, path)
	if leafKey != nil {
		i0, i1 := nodeBounds(in, leafKey)
		if value == nil {
			// delete leaf key
			return join(in[:i0], in[i1:]), nil
		}
		paste, err := Encode(map[string]any{path[n]: value}, 2)
		if err != nil {
			return nil, err
		}
		paste = addIndent(paste, leafKey.Column-1)
		return join(in[:i0], paste, in[i1:]), nil
	}

	if value == nil {
		// Key doesn't exist, deletion is no-op
		return in, nil
	}

	// Find the longest matching parent prefix
	for k := n; k >= 1; k-- {
		pKey, pVal := findNode(nodes, path[:k])
		if pKey != nil {
			v := buildNestedMap(path[k:], value)
			paste, err := Encode(v, 2)
			if err != nil {
				return nil, err
			}

			indent := pKey.Column + 1
			if pVal.Content != nil && len(pVal.Content) > 0 {
				indent = pVal.Content[0].Column - 1
			}

			paste = addIndent(paste, indent)
			_, i1 := nodeBounds(in, pKey)
			return join(in[:i1], paste, in[i1:]), nil
		}
	}

	// Top level key exists?
	if n == 0 {
		for i := 0; i < len(nodes); i += 2 {
			if nodes[i].Value == path[0] {
				i0, i1 := nodeBounds(in, nodes[i])
				if value == nil {
					return join(in[:i0], in[i1:]), nil
				}
				paste, err := Encode(map[string]any{path[0]: value}, 2)
				if err != nil {
					return nil, err
				}
				return join(in[:i0], paste, in[i1:]), nil
			}
		}
	}

	// No matching prefix found, add to end
	v := buildNestedMap(path, value)
	paste, err := Encode(v, 2)
	if err != nil {
		return nil, err
	}
	return join(in, paste), nil
}

func buildNestedMap(path []string, value any) any {
	if len(path) == 0 {
		return value
	}
	res := map[string]any{path[len(path)-1]: value}
	for i := len(path) - 2; i >= 0; i-- {
		res = map[string]any{path[i]: res}
	}
	return res
}

func findNode(nodes []*yaml.Node, keys []string) (key, value *yaml.Node) {
	for i, name := range keys {
		for j := 0; j < len(nodes); j += 2 {
			if nodes[j].Value == name {
				if i < len(keys)-1 {
					nodes = nodes[j+1].Content
					break
				}
				return nodes[j], nodes[j+1]
			}
		}
	}
	return nil, nil
}

func nodeBounds(in []byte, node *yaml.Node) (offset0, offset1 int) {
	// start from next line after node
	offset0 = lineOffset(in, node.Line)
	offset1 = lineOffset(in, node.Line+1)

	if offset1 < 0 {
		return offset0, len(in)
	}

	for i := offset1; i < len(in); {
		indent, length := parseLine(in[i:])
		if indent+1 != length {
			if node.Column < indent+1 {
				offset1 = i + length
			} else {
				break
			}
		}
		i += length
	}

	return
}

func join(items ...[]byte) []byte {
	n := len(items) - 1
	for _, b := range items {
		n += len(b)
	}

	buf := make([]byte, 0, n)
	for _, b := range items {
		if len(b) == 0 {
			continue
		}
		if n = len(buf); n > 0 && buf[n-1] != '\n' {
			buf = append(buf, '\n')
		}
		buf = append(buf, b...)
	}

	return buf
}

func addPrefix(src, pre []byte) (dst []byte) {
	for len(src) > 0 {
		dst = append(dst, pre...)
		i := bytes.IndexByte(src, '\n') + 1
		if i == 0 {
			dst = append(dst, src...)
			break
		}
		dst = append(dst, src[:i]...)
		src = src[i:]
	}

	return
}

func addIndent(in []byte, indent int) (dst []byte) {
	pre := make([]byte, indent)
	for i := range indent {
		pre[i] = ' '
	}
	return addPrefix(in, pre)
}

func lineOffset(in []byte, line int) (offset int) {
	for l := 1; ; l++ {
		if l == line {
			return offset
		}

		i := bytes.IndexByte(in[offset:], '\n') + 1
		if i == 0 {
			break
		}
		offset += i
	}
	return -1
}

func parseLine(b []byte) (indent int, length int) {
	prefix := true
	for ; length < len(b); length++ {
		switch b[length] {
		case ' ':
			if prefix {
				indent++
			}
		case '\n':
			length++
			return
		default:
			prefix = false
		}
	}
	return
}
