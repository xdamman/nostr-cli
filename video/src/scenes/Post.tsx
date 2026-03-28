import React from "react";
import { COLORS, SCENES } from "../styles";
import { useTerminalLines, type TerminalLine } from "../useTerminalLines";
import { TerminalRenderer } from "../TerminalRenderer";

const lines: TerminalLine[] = [
  {
    text: 'nostr post "Hello Nostr! 🤙 Posting from the terminal"',
    delay: 15,
    typewriter: true,
    charsPerFrame: 0.7,
    color: COLORS.command,
    prefix: "prompt",
  },
  { text: "", delay: 90 },
  { text: "✓ Published to 4/6 relays", delay: 105, color: COLORS.success },
  { text: "nevent1qqs8w7...", delay: 118, color: COLORS.npub, indent: 1 },
];

export const Post: React.FC = () => {
  const rendered = useTerminalLines(lines, SCENES.post.start);
  return <TerminalRenderer lines={rendered} />;
};
