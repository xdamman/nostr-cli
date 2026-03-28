import React from "react";
import { COLORS, SCENES } from "../styles";
import { useTerminalLines, type TerminalLine } from "../useTerminalLines";
import { TerminalRenderer } from "../TerminalRenderer";

const lines: TerminalLine[] = [
  {
    text: `nostr dm fiatjaf "Love what you're building with Nostr!"`,
    delay: 15,
    typewriter: true,
    charsPerFrame: 0.7,
    color: COLORS.command,
    prefix: "prompt",
  },
  { text: "✓ DM sent (NIP-04 encrypted)", delay: 100, color: COLORS.success },
];

export const DM: React.FC = () => {
  const rendered = useTerminalLines(lines, SCENES.dm.start);
  return <TerminalRenderer lines={rendered} />;
};
