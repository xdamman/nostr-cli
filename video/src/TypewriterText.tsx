import React from "react";
import { useCurrentFrame } from "remotion";
import { Cursor } from "./Cursor";
import { COLORS } from "./styles";

interface TypewriterTextProps {
  text: string;
  startFrame: number;
  charsPerFrame?: number;
  color?: string;
  showCursor?: boolean;
}

export const TypewriterText: React.FC<TypewriterTextProps> = ({
  text,
  startFrame,
  charsPerFrame = 0.8,
  color = COLORS.command,
  showCursor = true,
}) => {
  const frame = useCurrentFrame();
  const elapsed = frame - startFrame;

  if (elapsed < 0) return null;

  const charsToShow = Math.min(Math.floor(elapsed * charsPerFrame), text.length);
  const visibleText = text.slice(0, charsToShow);
  const isComplete = charsToShow >= text.length;

  return (
    <span>
      <span style={{ color }}>{visibleText}</span>
      {showCursor && !isComplete && <Cursor />}
    </span>
  );
};

// Utility: calculate how many frames a typewriter animation takes
export function typewriterDuration(text: string, charsPerFrame = 0.8): number {
  return Math.ceil(text.length / charsPerFrame);
}
