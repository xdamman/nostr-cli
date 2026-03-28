import React from "react";
import { COLORS, SCENES } from "../styles";
import { useTerminalLines, type TerminalLine } from "../useTerminalLines";
import { TerminalRenderer } from "../TerminalRenderer";

const lines: TerminalLine[] = [
  {
    text: "nostr accounts",
    delay: 15,
    typewriter: true,
    color: COLORS.command,
    prefix: "prompt",
  },
  { text: "1. alice  (npub1x7d...k3qf) ← active", delay: 50, color: COLORS.text, indent: 1 },
  { text: "2. mybot  (npub1m4k...9h2a)", delay: 60, color: COLORS.textDim, indent: 1 },
  { text: "", delay: 80 },
  {
    text: "nostr switch mybot",
    delay: 100,
    typewriter: true,
    color: COLORS.command,
    prefix: "prompt",
  },
  { text: "✓ Switched to mybot", delay: 135, color: COLORS.success },
];

export const Accounts: React.FC = () => {
  const rendered = useTerminalLines(lines, SCENES.accounts.start);
  return <TerminalRenderer lines={rendered} />;
};
