import React from "react";
import { useCurrentFrame } from "remotion";
import { COLORS, FONT } from "./styles";

export const Cursor: React.FC<{ visible?: boolean }> = ({ visible = true }) => {
  const frame = useCurrentFrame();
  const blink = Math.floor(frame / 15) % 2 === 0; // Blink every 0.5s

  if (!visible || !blink) return null;

  return (
    <span
      style={{
        display: "inline-block",
        width: FONT.size * 0.6,
        height: FONT.size * 1.2,
        backgroundColor: COLORS.text,
        verticalAlign: "text-bottom",
        marginLeft: 1,
      }}
    />
  );
};
