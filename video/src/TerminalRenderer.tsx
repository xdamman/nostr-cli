import React from "react";
import { useCurrentFrame } from "remotion";
import { COLORS, FONT } from "./styles";
import { Cursor } from "./Cursor";
import type { RenderedLine } from "./useTerminalLines";

interface TerminalRendererProps {
  lines: RenderedLine[];
}

export const TerminalRenderer: React.FC<TerminalRendererProps> = ({ lines }) => {
  const frame = useCurrentFrame();

  return (
    <div>
      {lines.map((line, i) => {
        if (!line.visible) return null;

        const displayText =
          line.typewriterProgress < 1
            ? line.text.slice(0, Math.floor(line.text.length * line.typewriterProgress))
            : line.text;

        return (
          <div
            key={i}
            style={{
              paddingLeft: line.indent * 20,
              minHeight: FONT.size * FONT.lineHeight,
              opacity: line.visible ? 1 : 0,
              transition: "opacity 0.1s",
            }}
          >
            {line.isPrompt && (
              <span style={{ color: COLORS.prompt }}>$ </span>
            )}
            <span style={{ color: line.color }}>{displayText}</span>
            {line.showCursor && <Cursor />}
          </div>
        );
      })}
    </div>
  );
};
