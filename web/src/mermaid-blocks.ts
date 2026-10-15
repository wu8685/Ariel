const diagramStart = /^\s*(?:flowchart|graph)\s+(?:TB|TD|BT|RL|LR)\s*$|^\s*(?:sequenceDiagram|stateDiagram(?:-v2)?|classDiagram|erDiagram|journey|gantt|mindmap|timeline)\s*$|^\s*pie(?:\s+showData)?\s*$/i;

type DiagramKind = "flowchart" | "sequence" | "generic";

function diagramKind(line: string): DiagramKind {
  const value = line.trim();
  if (/^(?:flowchart|graph)\b/i.test(value)) return "flowchart";
  if (/^sequenceDiagram$/i.test(value)) return "sequence";
  return "generic";
}

function flowchartLine(value: string): boolean {
  return /^(?:%%|subgraph\b|end\s*$|direction\b|style\b|classDef\b|class\b|click\b|linkStyle\b)/i.test(value)
    || /(?:-->|---|-.->|==>|~~~|<-->|o--o|x--x)/.test(value)
    || /^[\p{L}\p{N}_-]+\s*(?:@?\{|\[|\(|>)/u.test(value);
}

function sequenceLine(value: string): boolean {
  return /^(?:%%|participant\b|actor\b|Note\b|loop\b|alt\b|else\b|opt\b|par\b|and\b|critical\b|option\b|break\b|rect\b|activate\b|deactivate\b|autonumber\b|end\s*$)/i.test(value)
    || /^[\p{L}\p{N}_-]+\s*(?:--?>>|--?>|->>|->|--x|--\)|-x|-\))\s*[+\-]?[\p{L}\p{N}_-]+\s*:/u.test(value);
}

function genericMermaidLine(value: string): boolean {
  return /^(?:%%|title\b|accTitle\b|accDescr\b|dateFormat\b|axisFormat\b|excludes\b|includes\b|todayMarker\b|section\b|classDef\b|class\b|style\b|direction\b|state\b|note\b|end\s*$)/i.test(value)
    || /(?:-->|---|-.->|==>|\|o|o\||\}\|)/.test(value)
    || /^[\p{L}\p{N}_-]+\s*(?::|\{|\[|\()/u.test(value);
}

function belongsToDiagram(line: string, kind: DiagramKind): boolean {
  if (!line.trim()) return true;
  if (/^\s+\S/u.test(line)) return true;
  const value = line.trim();
  if (kind === "flowchart") return flowchartLine(value);
  if (kind === "sequence") return sequenceLine(value);
  return genericMermaidLine(value);
}

function fenceMarker(line: string): string | null {
  const match = /^\s{0,3}(`{3,}|~{3,})/.exec(line);
  return match?.[1] || null;
}

/**
 * Projects only confidently delimited bare Mermaid blocks into ordinary fenced
 * Markdown. Existing fences are opaque so code examples are never rewritten.
 */
export function projectBareMermaidBlocks(text: string): string {
  const lines = text.split("\n");
  const output: string[] = [];
  let openFence: string | null = null;

  for (let index = 0; index < lines.length;) {
    const line = lines[index];
    const marker = fenceMarker(line);
    if (marker) {
      if (!openFence) openFence = marker;
      else if (marker[0] === openFence[0] && marker.length >= openFence.length) openFence = null;
      output.push(line);
      index += 1;
      continue;
    }
    if (openFence || !diagramStart.test(line)) {
      output.push(line);
      index += 1;
      continue;
    }

    const kind = diagramKind(line);
    const block = [line];
    const pendingBlank: string[] = [];
    let cursor = index + 1;
    let statementCount = 0;
    while (cursor < lines.length) {
      const candidate = lines[cursor];
      if (!candidate.trim()) {
        pendingBlank.push(candidate);
        cursor += 1;
        continue;
      }
      if (!belongsToDiagram(candidate, kind)) break;
      block.push(...pendingBlank, candidate);
      pendingBlank.length = 0;
      statementCount += 1;
      cursor += 1;
    }

    if (statementCount === 0) {
      output.push(line);
      index += 1;
      continue;
    }
    output.push("```mermaid", ...block, "```", ...pendingBlank);
    index = cursor;
  }
  return output.join("\n");
}
