package filecontext

// SVG is an explicitly negotiated, static image format, never a document or
// HTML fragment. Rebuild a small XML subset instead of forwarding raw markup.
import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const svgNS = "http://www.w3.org/2000/svg"
const xlinkNS = "http://www.w3.org/1999/xlink"

var svgTags = words("svg g defs clipPath path rect circle ellipse line polyline polygon text tspan title desc linearGradient radialGradient stop use")
var svgAttrs = words("id viewBox preserveAspectRatio version x y x1 x2 y1 y2 width height rx ry r cx cy fx fy dx dy d points transform pathLength opacity fill fill-opacity fill-rule stroke stroke-opacity stroke-width stroke-linecap stroke-linejoin stroke-miterlimit stroke-dasharray stroke-dashoffset clip-path clip-rule clipPathUnits gradientUnits gradientTransform spreadMethod offset stop-color stop-opacity font-family font-size font-weight font-style text-anchor dominant-baseline alignment-baseline letter-spacing word-spacing textLength lengthAdjust rotate vector-effect")
var svgStyle = words("opacity fill fill-opacity fill-rule stroke stroke-opacity stroke-width stroke-linecap stroke-linejoin stroke-miterlimit stroke-dasharray stroke-dashoffset clip-path clip-rule stop-color stop-opacity font-family font-size font-weight font-style text-anchor dominant-baseline alignment-baseline letter-spacing word-spacing")
var svgID = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.:-]{0,255}$`)
var svgNumber = regexp.MustCompile(`[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?`)
var svgLength = regexp.MustCompile(`^([+]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?)(px|pt|pc|in|cm|mm)?$`)
var svgTransform = regexp.MustCompile(`^(?:(?:matrix|translate|scale|rotate|skewX|skewY)\([0-9eE+.,\s-]+\)[\s,]*)+$`)
var svgPaint = regexp.MustCompile(`^(?:#[a-fA-F0-9]{3,8}|[A-Za-z]+|(?:rgb|rgba|hsl|hsla)\([0-9+.,%\s-]+\))$`)

func words(value string) map[string]bool {
	result := map[string]bool{}
	for _, word := range strings.Fields(value) {
		result[word] = true
	}
	return result
}

type svgNode struct {
	tag                   string
	attrs                 []xml.Attr
	children              []*svgNode
	content               []svgContent
	refs                  []svgRef
	ownCost               int
	geometryCost          int
	state, cost, refDepth int
}
type svgContent struct {
	text  string
	child *svgNode
}
type svgRef struct{ id, kind string }

func finiteSVGNumbers(value string) bool {
	for _, token := range svgNumber.FindAllString(value, -1) {
		n, err := strconv.ParseFloat(token, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > 1e9 {
			return false
		}
	}
	return true
}

func svgAttribute(node *svgNode, name, value string) bool {
	value = strings.TrimSpace(value)
	if name == "href" {
		if node.tag != "use" && node.tag != "linearGradient" && node.tag != "radialGradient" {
			return false
		}
		if !strings.HasPrefix(value, "#") || !svgID.MatchString(value[1:]) {
			return false
		}
		node.refs = append(node.refs, svgRef{value[1:], "href"})
	} else if name == "id" {
		if !svgID.MatchString(value) {
			return false
		}
	} else if name == "space" {
		if value != "preserve" && value != "default" {
			return false
		}
	} else {
		if !svgAttrs[name] || value == "" || strings.IndexFunc(value, unicode.IsControl) >= 0 || strings.ContainsAny(value, `\<>@;!"'`) {
			return false
		}
		if strings.HasPrefix(value, "url(") {
			if name != "fill" && name != "stroke" && name != "clip-path" {
				return false
			}
			if !strings.HasSuffix(value, ")") {
				return false
			}
			id := strings.TrimSpace(value[4 : len(value)-1])
			if !strings.HasPrefix(id, "#") || !svgID.MatchString(id[1:]) {
				return false
			}
			value = "url(" + id + ")"
			node.refs = append(node.refs, svgRef{id[1:], name})
		} else if name == "transform" || name == "gradientTransform" {
			if !svgTransform.MatchString(value) || !finiteSVGNumbers(value) {
				return false
			}
		} else if name == "fill" || name == "stroke" || name == "stop-color" {
			if !svgPaint.MatchString(value) || strings.Contains(value, "(") && !finiteSVGNumbers(value) {
				return false
			}
		} else if strings.ContainsAny(value, "()/:") || name != "font-family" && !finiteSVGNumbers(value) {
			return false
		}
	}
	attributeName := xml.Name{Local: name}
	if name == "space" {
		attributeName.Space = "http://www.w3.org/XML/1998/namespace"
	}
	node.attrs = append(node.attrs, xml.Attr{Name: attributeName, Value: value})
	return true
}

func svgPixels(value string) float64 {
	parts := svgLength.FindStringSubmatch(strings.TrimSpace(value))
	if parts == nil {
		return 0
	}
	n, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || n <= 0 || math.IsInf(n, 0) || math.IsNaN(n) {
		return 0
	}
	factor := map[string]float64{"": 1, "px": 1, "pt": 96.0 / 72, "pc": 16, "in": 96, "cm": 96 / 2.54, "mm": 96 / 25.4}[parts[2]]
	return n * factor
}

func svgDimensions(root *svgNode) (int, int, error) {
	values := map[string]string{}
	for _, attr := range root.attrs {
		values[attr.Name.Local] = attr.Value
	}
	w, h := svgPixels(values["width"]), svgPixels(values["height"])
	// A viewBox can supply a missing dimension, never repair an explicitly
	// invalid, zero, negative, or viewport-relative length.
	if values["width"] != "" && w <= 0 || values["height"] != "" && h <= 0 {
		return 0, 0, fail(CodeUnsupportedFileType)
	}
	var vbW, vbH float64
	if box := values["viewBox"]; box != "" {
		parts := strings.Fields(strings.ReplaceAll(box, ",", " "))
		if len(parts) != 4 {
			return 0, 0, fail(CodeUnsupportedFileType)
		}
		var numbers [4]float64
		for i, part := range parts {
			n, err := strconv.ParseFloat(part, 64)
			if err != nil || math.IsInf(n, 0) || math.IsNaN(n) || math.Abs(n) > 1e9 {
				return 0, 0, fail(CodeUnsupportedFileType)
			}
			numbers[i] = n
		}
		vbW, vbH = numbers[2], numbers[3]
		if vbW <= 0 || vbH <= 0 {
			return 0, 0, fail(CodeUnsupportedFileType)
		}
	}
	if w == 0 && h == 0 {
		w, h = vbW, vbH
	} else if vbW > 0 && vbH > 0 {
		if w == 0 {
			w = h * vbW / vbH
		}
		if h == 0 {
			h = w * vbH / vbW
		}
	}
	if w <= 0 || h <= 0 {
		return 0, 0, fail(CodeUnsupportedFileType)
	}
	if w > 8192 || h > 8192 || math.Ceil(w)*math.Ceil(h) > 12_000_000 {
		return 0, 0, fail(CodeFileTooLarge)
	}
	width, height := int(math.Ceil(w)), int(math.Ceil(h))
	attrs := []xml.Attr{{Name: xml.Name{Local: "xmlns"}, Value: svgNS}, {Name: xml.Name{Local: "width"}, Value: strconv.Itoa(width)}, {Name: xml.Name{Local: "height"}, Value: strconv.Itoa(height)}}
	for _, attr := range root.attrs {
		if attr.Name.Local != "width" && attr.Name.Local != "height" {
			attrs = append(attrs, attr)
		}
	}
	root.attrs = attrs
	return width, height, nil
}

func staticSVGImage(ctx context.Context, body []byte) ([]byte, int, int, error) {
	bad := func() ([]byte, int, int, error) { return nil, 0, 0, fail(CodeUnsupportedFileType) }
	if len(body) > MaxAssetBytes {
		return nil, 0, 0, fail(CodeFileTooLarge)
	}
	if !utf8.Valid(body) {
		return bad()
	}
	d := xml.NewDecoder(bytes.NewReader(body))
	var root *svgNode
	stack := []*svgNode{}
	ids := map[string]*svgNode{}
	nodes, coordinates := 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, 0, 0, err
		}
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return bad()
		}
		switch t := token.(type) {
		case xml.StartElement:
			nodes++
			if nodes > 20_000 || len(stack) >= 64 || len(t.Attr) > 64 {
				return nil, 0, 0, fail(CodeFileTooLarge)
			}
			if !svgTags[t.Name.Local] || t.Name.Space != svgNS && t.Name.Space != "" || t.Name.Local == "svg" && root != nil {
				return bad()
			}
			node := &svgNode{tag: t.Name.Local, ownCost: 1}
			seen := map[string]bool{}
			for _, attr := range t.Attr {
				if len(attr.Value) > 64<<10 {
					return nil, 0, 0, fail(CodeFileTooLarge)
				}
				numbers := len(svgNumber.FindAllString(attr.Value, 1_000_001-coordinates))
				coordinates += numbers
				if coordinates > 1_000_000 {
					return nil, 0, 0, fail(CodeFileTooLarge)
				}
				node.ownCost += numbers
				if attr.Name.Space == "xmlns" || attr.Name.Space == "" && attr.Name.Local == "xmlns" {
					if attr.Value != svgNS && attr.Value != xlinkNS {
						return bad()
					}
					continue
				}
				name := attr.Name.Local
				if attr.Name.Space != "" && !(attr.Name.Space == xlinkNS && name == "href") && !(attr.Name.Space == "http://www.w3.org/XML/1998/namespace" && name == "space") {
					return bad()
				}
				if name == "data-text" {
					continue
				} // PDF glyph accessibility metadata is not rendered or quoted.
				if seen[name] {
					return bad()
				}
				seen[name] = true
				if name == "style" {
					for _, decl := range strings.Split(attr.Value, ";") {
						if strings.TrimSpace(decl) == "" {
							continue
						}
						pair := strings.SplitN(decl, ":", 2)
						if len(pair) != 2 || !svgStyle[strings.TrimSpace(pair[0])] {
							return bad()
						}
						prop := strings.TrimSpace(pair[0])
						if seen[prop] || !svgAttribute(node, prop, pair[1]) {
							return bad()
						}
						seen[prop] = true
					}
				} else if !svgAttribute(node, name, attr.Value) {
					return bad()
				}
			}
			for _, attr := range node.attrs {
				if attr.Name.Local == "id" {
					if ids[attr.Value] != nil {
						return bad()
					}
					ids[attr.Value] = node
				}
			}
			if len(stack) == 0 {
				if root != nil || node.tag != "svg" {
					return bad()
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
				parent.content = append(parent.content, svgContent{child: node})
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) == 0 {
				return bad()
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(t)) != "" {
					return bad()
				}
				continue
			}
			parent := stack[len(stack)-1]
			if strings.TrimSpace(string(t)) != "" && parent.tag != "text" && parent.tag != "tspan" && parent.tag != "title" && parent.tag != "desc" {
				return bad()
			}
			// Text can also be amplified by use graphs, without adding any
			// numeric tokens or XML elements. Charge its UTF-8 bytes as work.
			parent.ownCost += len(t)
			parent.content = append(parent.content, svgContent{text: string(t)})
		case xml.Directive:
			return bad()
		case xml.ProcInst:
			if t.Target != "xml" || root != nil {
				return bad()
			}
		}
	}
	if root == nil || len(stack) != 0 {
		return bad()
	}
	width, height, err := svgDimensions(root)
	if err != nil {
		return nil, 0, 0, err
	}
	var check func(*svgNode, int) error
	check = func(node *svgNode, chain int) error {
		if chain > 16 {
			return fail(CodeFileTooLarge)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if node.state == 1 {
			return fail(CodeUnsupportedFileType)
		}
		if node.state == 2 {
			if chain+node.refDepth > 16 {
				return fail(CodeFileTooLarge)
			}
			return nil
		}
		node.state = 1
		node.cost = 1
		node.geometryCost = node.ownCost
		for _, child := range node.children {
			if err := check(child, chain); err != nil {
				return err
			}
			node.cost += child.cost
			node.geometryCost += child.geometryCost
			node.refDepth = max(node.refDepth, child.refDepth)
			if node.cost > 100_000 || node.geometryCost > 1_000_000 {
				return fail(CodeFileTooLarge)
			}
		}
		for _, ref := range node.refs {
			target := ids[ref.id]
			if target == nil {
				return fail(CodeUnsupportedFileType)
			}
			if ref.kind == "clip-path" && target.tag != "clipPath" || (ref.kind == "fill" || ref.kind == "stroke") && target.tag != "linearGradient" && target.tag != "radialGradient" {
				return fail(CodeUnsupportedFileType)
			}
			if err := check(target, chain+1); err != nil {
				return err
			}
			node.cost += target.cost
			node.geometryCost += target.geometryCost
			node.refDepth = max(node.refDepth, target.refDepth+1)
			if node.cost > 100_000 || node.geometryCost > 1_000_000 || node.refDepth > 16 {
				return fail(CodeFileTooLarge)
			}
		}
		node.state = 2
		return nil
	}
	if err = check(root, 0); err != nil {
		return nil, 0, 0, err
	}
	var output bytes.Buffer
	e := xml.NewEncoder(&output)
	var write func(*svgNode) error
	write = func(node *svgNode) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := xml.StartElement{Name: xml.Name{Local: node.tag}, Attr: node.attrs}
		if err := e.EncodeToken(start); err != nil {
			return err
		}
		for _, item := range node.content {
			if item.child != nil {
				if err := write(item.child); err != nil {
					return err
				}
			} else if err := e.EncodeToken(xml.CharData(item.text)); err != nil {
				return err
			}
		}
		if output.Len() > MaxAssetBytes {
			return fail(CodeFileTooLarge)
		}
		return e.EncodeToken(start.End())
	}
	if err = write(root); err != nil {
		return nil, 0, 0, err
	}
	if err = e.Flush(); err != nil {
		return bad()
	}
	if output.Len() > MaxAssetBytes {
		return nil, 0, 0, fail(CodeFileTooLarge)
	}
	return output.Bytes(), width, height, nil
}
