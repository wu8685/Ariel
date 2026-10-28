// Defense in depth for Agent-canonicalized SVG. This DOM is never attached to
// the page; accepted bytes are only decoded in the browser's image context.
const tags = new Set(
  "svg g defs clipPath path rect circle ellipse line polyline polygon text tspan title desc linearGradient radialGradient stop use".split(
    " ",
  ),
);
const attributes = new Set(
  "id viewBox preserveAspectRatio version x y x1 x2 y1 y2 width height rx ry r cx cy fx fy dx dy d points transform pathLength opacity fill fill-opacity fill-rule stroke stroke-opacity stroke-width stroke-linecap stroke-linejoin stroke-miterlimit stroke-dasharray stroke-dashoffset clip-path clip-rule clipPathUnits gradientUnits gradientTransform spreadMethod offset stop-color stop-opacity font-family font-size font-weight font-style text-anchor dominant-baseline alignment-baseline letter-spacing word-spacing textLength lengthAdjust rotate vector-effect".split(
    " ",
  ),
);
const namespace = "http://www.w3.org/2000/svg";
const idPattern = /^[A-Za-z_][A-Za-z0-9_.:-]{0,255}$/u;
const numberPattern = /[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?/gu;
const transformPattern =
  /^(?:(?:matrix|translate|scale|rotate|skewX|skewY)\([0-9eE+.,\s-]+\)[\s,]*)+$/u;
const paintPattern =
  /^(?:#[a-fA-F0-9]{3,8}|[A-Za-z]+|(?:rgb|rgba|hsl|hsla)\([0-9+.,%\s-]+\))$/u;
const invalid = (): never => {
  throw new Error("SVG 图片内容无效或不满足静态安全要求");
};

export function staticSVGInfo(bytes: Uint8Array): {
  width: number;
  height: number;
} {
  if (bytes.length > 4 * 1024 * 1024) return invalid();
  const source = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  if (/<[!?]/u.test(source)) return invalid();
  // Cheap preflight before constructing an XML tree, including hostile depth.
  let depth = 0,
    count = 0;
  for (const match of source.matchAll(/<(\/?)([^\s/>]+)[^>]*>/gu)) {
    if (!tags.has(match[2])) return invalid();
    if (match[1]) depth--;
    else {
      if (++count > 20_000 || ++depth > 64) return invalid();
      if (match[0].endsWith("/>")) depth--;
    }
    if (depth < 0) return invalid();
  }
  if (depth !== 0) return invalid();
  const doc = new DOMParser().parseFromString(source, "image/svg+xml"),
    root = doc.documentElement;
  if (
    doc.querySelector("parsererror") ||
    root.localName !== "svg" ||
    root.namespaceURI !== namespace
  )
    return invalid();
  const all = [root, ...root.querySelectorAll("*")],
    ids = new Map<string, Element>();
  const references = new Map<Element, Array<{ id: string; kind: string }>>(),
    weights = new Map<Element, number>();
  let coordinates = 0;
  if (all.length > 20_000) return invalid();
  for (const node of all) {
    if (
      node.namespaceURI !== namespace ||
      !tags.has(node.localName) ||
      (node !== root && node.localName === "svg") ||
      node.attributes.length > 64
    )
      return invalid();
    const refs: Array<{ id: string; kind: string }> = [];
    references.set(node, refs);
    let ownCost = 1;
    for (const attribute of [...node.attributes]) {
      const name = attribute.localName,
        value = attribute.value;
      if (value.length > 64 * 1024) return invalid();
      let numbers = 0;
      for (const _number of value.matchAll(numberPattern)) {
        if (++coordinates > 1_000_000) return invalid();
        numbers++;
      }
      ownCost += numbers;
      if (attribute.name === "xmlns" && node === root && value === namespace)
        continue;
      if (
        attribute.namespaceURI === "http://www.w3.org/XML/1998/namespace" &&
        name === "space" &&
        ["default", "preserve"].includes(value)
      )
        continue;
      if (attribute.namespaceURI) return invalid();
      if (name === "id") {
        if (!idPattern.test(value) || ids.has(value)) return invalid();
        ids.set(value, node);
        continue;
      }
      if (name === "href") {
        if (
          !["use", "linearGradient", "radialGradient"].includes(
            node.localName,
          ) ||
          !value.startsWith("#") ||
          !idPattern.test(value.slice(1))
        )
          return invalid();
        refs.push({ id: value.slice(1), kind: name });
        continue;
      }
      if (
        !attributes.has(name) ||
        !value ||
        /[\u0000-\u001f\u007f\\<>@;!"']/u.test(value)
      )
        return invalid();
      const finite = () =>
        ![...value.matchAll(numberPattern)].some(
          (match) =>
            !Number.isFinite(Number(match[0])) ||
            Math.abs(Number(match[0])) > 1e9,
        );
      if (value.startsWith("url(")) {
        const match = /^url\(#([A-Za-z_][A-Za-z0-9_.:-]{0,255})\)$/u.exec(
          value,
        );
        if (!match || !["fill", "stroke", "clip-path"].includes(name))
          return invalid();
        refs.push({ id: match[1], kind: name });
      } else if (["transform", "gradientTransform"].includes(name)) {
        if (!transformPattern.test(value) || !finite()) return invalid();
      } else if (["fill", "stroke", "stop-color"].includes(name)) {
        if (!paintPattern.test(value) || (value.includes("(") && !finite()))
          return invalid();
      } else if (/[()/:]/u.test(value) || (name !== "font-family" && !finite()))
        return invalid();
    }
    for (const child of [...node.childNodes]) {
      if (child.nodeType !== Node.TEXT_NODE) continue;
      const text = child.textContent || "";
      if (
        text.trim() &&
        !["text", "tspan", "title", "desc"].includes(node.localName)
      )
        return invalid();
      ownCost += new TextEncoder().encode(text).length;
    }
    weights.set(node, ownCost);
  }
  const width = Number(root.getAttribute("width")),
    height = Number(root.getAttribute("height"));
  if (
    !Number.isInteger(width) ||
    !Number.isInteger(height) ||
    width < 1 ||
    height < 1 ||
    width > 8192 ||
    height > 8192 ||
    width * height > 12_000_000
  )
    return invalid();
  const states = new Map<Element, number>(),
    memo = new Map<
      Element,
      { cost: number; geometry: number; depth: number }
    >();
  function visit(
    node: Element,
    chain: number,
  ): { cost: number; geometry: number; depth: number } {
    if (chain > 16 || states.get(node) === 1) return invalid();
    const found = memo.get(node);
    if (found) {
      if (chain + found.depth > 16) return invalid();
      return found;
    }
    states.set(node, 1);
    let cost = 1,
      geometry = weights.get(node) || 1,
      depth = 0;
    for (const child of [...node.children]) {
      const value = visit(child, chain);
      cost += value.cost;
      geometry += value.geometry;
      depth = Math.max(depth, value.depth);
      if (cost > 100_000 || geometry > 1_000_000) return invalid();
    }
    for (const ref of references.get(node) || []) {
      const target = ids.get(ref.id);
      if (
        !target ||
        (ref.kind === "clip-path" && target.localName !== "clipPath") ||
        (["fill", "stroke"].includes(ref.kind) &&
          !["linearGradient", "radialGradient"].includes(target.localName))
      )
        return invalid();
      const value = visit(target, chain + 1);
      cost += value.cost;
      geometry += value.geometry;
      depth = Math.max(depth, value.depth + 1);
      if (cost > 100_000 || geometry > 1_000_000 || depth > 16)
        return invalid();
    }
    states.set(node, 2);
    const result = { cost, geometry, depth };
    memo.set(node, result);
    return result;
  }
  visit(root, 0);
  return { width, height };
}
