import React from "react";
import { COLORS, SCENES } from "../styles";
import { useTerminalLines, type TerminalLine } from "../useTerminalLines";
import { TerminalRenderer } from "../TerminalRenderer";

const lines: TerminalLine[] = [
  {
    text: "nostr",
    delay: 15,
    typewriter: true,
    color: COLORS.command,
    prefix: "prompt",
  },
  { text: "", delay: 40 },
  {
    text: "/switch",
    delay: 55,
    typewriter: true,
    color: COLORS.command,
    prefix: "user",
    prefixText: "xdamman",
  },
  { text: "", delay: 85 },
  { text: "Switch account:", delay: 95, color: COLORS.text },
  { text: "→ xdamman (npub1x7d...k3qf) ← active", delay: 108, color: COLORS.label, indent: 1 },
  { text: "  mybot   (npub1m4k...9h2a)", delay: 118, color: COLORS.textDim, indent: 1 },
  { text: "", delay: 140 },
  { text: "✓ Switched to mybot", delay: 155, color: COLORS.success },
  { text: "", delay: 170 },
  {
    text: "",
    delay: 185,
    color: COLORS.text,
    prefix: "user",
    prefixText: "mybot",
  },
];

export const Accounts: React.FC = () => {
  const rendered = useTerminalLines(lines, SCENES.accounts.start);
  return <TerminalRenderer lines={rendered} />;
};
