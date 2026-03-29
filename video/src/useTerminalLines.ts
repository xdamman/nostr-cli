import { useCurrentFrame } from "remotion";
import { COLORS } from "./styles";

export interface TerminalLine {
  text: string;
  color?: string;
  indent?: number;
  delay: number; // frame at which this line appears (relative to scene start)
  typewriter?: boolean; // whether to use typewriter effect
  charsPerFrame?: number;
  prefix?: "prompt" | "user" | "none";
  prefixText?: string; // custom prefix text (e.g. username for "user" prefix)
}

export interface RenderedLine {
  text: string;
  color: string;
  indent: number;
  visible: boolean;
  typewriterProgress: number; // 0-1
  showCursor: boolean;
  isPrompt: boolean;
  prefixType: "prompt" | "user" | "none";
  prefixText?: string;
}

export function useTerminalLines(
  lines: TerminalLine[],
  sceneStartFrame: number
): RenderedLine[] {
  const frame = useCurrentFrame();
  const relativeFrame = frame - sceneStartFrame;

  return lines.map((line, i) => {
    const elapsed = relativeFrame - line.delay;
    const visible = elapsed >= 0;

    let typewriterProgress = 1;
    let showCursor = false;

    if (line.typewriter && visible) {
      const cpf = line.charsPerFrame ?? 0.8;
      const charsToShow = Math.floor(elapsed * cpf);
      typewriterProgress = Math.min(charsToShow / line.text.length, 1);
      showCursor = typewriterProgress < 1;

      // Also show cursor briefly after completion
      if (typewriterProgress >= 1) {
        const framesAfterComplete = elapsed - Math.ceil(line.text.length / cpf);
        showCursor = framesAfterComplete < 15; // Show cursor for 0.5s after typing
      }
    }

    // Check if next line exists and is visible — if so, no cursor on this line
    if (i < lines.length - 1) {
      const nextLineElapsed = relativeFrame - lines[i + 1].delay;
      if (nextLineElapsed >= 0) {
        showCursor = false;
      }
    }

    return {
      text: line.text,
      color: line.color ?? COLORS.text,
      indent: line.indent ?? 0,
      visible,
      typewriterProgress,
      showCursor,
      isPrompt: line.prefix === "prompt",
      prefixType: (line.prefix as "prompt" | "user" | "none") ?? "none",
      prefixText: line.prefixText,
    };
  });
}
