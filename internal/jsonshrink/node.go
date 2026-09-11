// Package jsonshrink parses JSON into an order-preserving tree and shrinks it:
// field selection, noise removal, list truncation, and compact or tabular output.
package jsonshrink

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

type Kind int

const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

// Node is a JSON value. Objects keep their key order, which encoding/json maps
// would lose. For Bool/Number, Str holds the literal text.
type Node struct {
	Kind Kind
	Str  string
	Keys []string // Object keys, parallel to Vals
	Vals []*Node  // Object values or Array elements
}

func (n *Node) Get(key string) *Node {
	for i, k := range n.Keys {
		if k == key {
			return n.Vals[i]
		}
	}
	return nil
}

func strNode(s string) *Node { return &Node{Kind: String, Str: s} }

// Parse decodes JSON. A stream of several top-level values (JSON Lines / NDJSON)
// becomes an array of them.
func Parse(data []byte) (*Node, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var vals []*Node
	for {
		n, err := decodeValue(dec)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		vals = append(vals, n)
	}
	switch len(vals) {
	case 0:
		return nil, errors.New("empty input")
	case 1:
		return vals[0], nil
	}
	return &Node{Kind: Array, Vals: vals}, nil
}

func decodeValue(dec *json.Decoder) (*Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return fromToken(dec, tok)
}

func fromToken(dec *json.Decoder, tok json.Token) (*Node, error) {
	switch v := tok.(type) {
	case nil:
		return &Node{Kind: Null}, nil
	case bool:
		return &Node{Kind: Bool, Str: strconv.FormatBool(v)}, nil
	case json.Number:
		return &Node{Kind: Number, Str: v.String()}, nil
	case string:
		return strNode(v), nil
	case json.Delim:
		switch v {
		case '[':
			n := &Node{Kind: Array}
			for dec.More() {
				el, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				n.Vals = append(n.Vals, el)
			}
			_, err := dec.Token() // ]
			return n, err
		case '{':
			n := &Node{Kind: Object}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := kt.(string)
				val, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				n.Keys = append(n.Keys, key)
				n.Vals = append(n.Vals, val)
			}
			_, err := dec.Token() // }
			return n, err
		}
	}
	return nil, fmt.Errorf("unexpected JSON token %v", tok)
}

// Compact is the minified JSON of n.
func (n *Node) Compact() string {
	var b bytes.Buffer
	n.writeCompact(&b)
	return b.String()
}

func (n *Node) writeCompact(b *bytes.Buffer) {
	switch n.Kind {
	case Null:
		b.WriteString("null")
	case Bool, Number:
		b.WriteString(n.Str)
	case String:
		writeString(b, n.Str)
	case Array:
		b.WriteByte('[')
		for i, v := range n.Vals {
			if i > 0 {
				b.WriteByte(',')
			}
			v.writeCompact(b)
		}
		b.WriteByte(']')
	case Object:
		b.WriteByte('{')
		for i, k := range n.Keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeString(b, k)
			b.WriteByte(':')
			n.Vals[i].writeCompact(b)
		}
		b.WriteByte('}')
	}
}

func writeString(b *bytes.Buffer, s string) {
	// Default JSON encoding escapes <, >, & as < etc. — that only costs tokens.
	enc := json.NewEncoder(b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	b.Truncate(b.Len() - 1) // Encode appends a newline
}

// compactLen approximates len(n.Compact()), giving up as soon as it passes limit.
func compactLen(n *Node, limit int) int {
	switch n.Kind {
	case Null:
		return 4
	case Bool, Number:
		return len(n.Str)
	case String:
		return len(n.Str) + 2
	}
	total := 2
	for i, v := range n.Vals {
		if i > 0 {
			total++
		}
		if n.Kind == Object {
			total += len(n.Keys[i]) + 3
		}
		if total += compactLen(v, limit-total); total > limit {
			return total
		}
	}
	return total
}

// inlineWidth is the widest a container may be and still print on one line.
const inlineWidth = 100

// Pretty prints n compactly but readably: containers that fit in inlineWidth
// characters stay on one line; bigger ones put each child on its own line,
// indented by one space per level. This is close to minified JSON in tokens but
// can be paged at line boundaries.
func (n *Node) Pretty() string {
	var b bytes.Buffer
	n.writePretty(&b, 0)
	return b.String()
}

func (n *Node) writePretty(b *bytes.Buffer, depth int) {
	if (n.Kind != Array && n.Kind != Object) || len(n.Vals) == 0 {
		n.writeCompact(b)
		return
	}
	if compactLen(n, inlineWidth-depth) <= inlineWidth-depth {
		n.writeCompact(b)
		return
	}
	open, close := byte('['), byte(']')
	if n.Kind == Object {
		open, close = '{', '}'
	}
	b.WriteByte(open)
	for i, v := range n.Vals {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
		b.Write(bytes.Repeat([]byte{' '}, depth+1))
		if n.Kind == Object {
			writeString(b, n.Keys[i])
			b.WriteByte(':')
		}
		v.writePretty(b, depth+1)
	}
	b.WriteByte('\n')
	b.Write(bytes.Repeat([]byte{' '}, depth))
	b.WriteByte(close)
}
